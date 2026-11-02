package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/firehydrant/fhcli/pkg/auth"
)

// storeLogin saves c as the "us" login in a fresh temp XDG_CONFIG_HOME.
func storeLogin(t *testing.T, c *auth.Credentials) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := auth.DefaultStore().Save(c); err != nil {
		t.Fatal(err)
	}
}

func loginCreds(host string, expiry time.Time) *auth.Credentials {
	return &auth.Credentials{
		AccessToken: "login-at", RefreshToken: "login-rt", Expiry: expiry,
		Region: "us", AppHost: host, APIHost: host, ClientID: "cid",
		User: "Dylan", Email: "dylan@example.com", Organization: "FireHydrant", OrganizationID: "org-1",
	}
}

// authServer records the Authorization header of each API request and
// serves an incident, a token refresh, and a 401 route.
type authServer struct {
	*httptest.Server
	headers        []string
	refreshInvalid bool
}

func newAuthServer(t *testing.T) *authServer {
	t.Helper()
	s := &authServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/incidents/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.headers = append(s.headers, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "name": "DB down"})
	})
	mux.HandleFunc("GET /v1/unauthorized", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"The bearer token you provided is invalid or expired."}`))
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if s.refreshInvalid || r.Form.Get("refresh_token") != "login-rt" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "refreshed-at", "refresh_token": "refreshed-rt", "token_type": "Bearer", "expires_in": 7200})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *authServer) lastHeader() string {
	if len(s.headers) == 0 {
		return ""
	}
	return s.headers[len(s.headers)-1]
}

func TestStoredLoginSendsBearerToLoginHost(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "incidents", "get", "inc-1")

	if err != nil {
		t.Fatal(err)
	}
	if srv.lastHeader() != "Bearer login-at" {
		t.Errorf("Authorization = %q, want the login's bearer token", srv.lastHeader())
	}
}

func TestStoredLoginWorksForAPICommands(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "api", "incidents", "get-incident", "inc-1")

	if err != nil {
		t.Fatal(err)
	}
	if srv.lastHeader() != "Bearer login-at" {
		t.Errorf("Authorization = %q", srv.lastHeader())
	}
}

func TestTokenFlagBeatsLogin(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))

	_, err := runCmd(t, srv.URL, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "test-token" {
		t.Errorf("Authorization = %q, err = %v; want --token to win", srv.lastHeader(), err)
	}
}

func TestEnvTokenBeatsLogin(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))
	t.Setenv("FIREHYDRANT_API_TOKEN", "env-token")

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--api-host", srv.URL, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "env-token" {
		t.Errorf("Authorization = %q, err = %v; want the env token to win", srv.lastHeader(), err)
	}
}

func TestEnvTokenIsReadByEveryRootCommand(t *testing.T) {
	srv := newAuthServer(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := runCmd(t, srv.URL, "incidents", "get", "inc-1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIREHYDRANT_API_TOKEN", "env-token")

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--api-host", srv.URL, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "env-token" {
		t.Errorf("Authorization = %q, err = %v; a second root must still read FIREHYDRANT_API_TOKEN", srv.lastHeader(), err)
	}
}

// A config file can be planted in world-writable /tmp, so its apiKey must
// not take over a logged-in session (nor, with its apiHost, send that
// session's requests elsewhere).
func TestLoginBeatsConfigFileToken(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))
	cfg := filepath.Join(t.TempDir(), "firehydrant.cfg")
	os.WriteFile(cfg, []byte("apiKey: config-token\napiHost: http://127.0.0.1:1\n"), 0o600)
	t.Setenv("FH_API_KEY", "")
	os.Unsetenv("FH_API_KEY")
	t.Setenv("FH_API_HOST", "")
	os.Unsetenv("FH_API_HOST")

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--config", cfg, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "Bearer login-at" {
		t.Errorf("Authorization = %q, err = %v, stderr = %s; want the login to win over a config-file apiKey", srv.lastHeader(), err, stderr)
	}
}

func TestConfigFileTokenIsUsedWithoutLogin(t *testing.T) {
	srv := newAuthServer(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := filepath.Join(t.TempDir(), "firehydrant.cfg")
	os.WriteFile(cfg, []byte("apiKey: config-token\n"), 0o600)
	t.Setenv("FH_API_KEY", "")
	os.Unsetenv("FH_API_KEY")

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--config", cfg, "--api-host", srv.URL, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "config-token" {
		t.Errorf("Authorization = %q, err = %v; with no login the config token should be used", srv.lastHeader(), err)
	}
}

func TestInitIgnoresExistingConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.WriteFile(filepath.Join(home, "firehydrant.cfg"), []byte("apiKey: other-org-key\napiHost: other.example\nignoreErrors: false\n"), 0o600)
	t.Setenv("FH_API_KEY", "")
	os.Unsetenv("FH_API_KEY")
	out := filepath.Join(t.TempDir(), "new.cfg")

	if _, err := runCmdNoToken(t, "--config", out, "init", "--service", "x"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "other-org-key") || strings.Contains(string(data), "other.example") || !strings.Contains(string(data), "ignoreErrors: true") {
		t.Errorf("init copied values from an existing config file:\n%s", data)
	}
}

func TestDebugDumpsRequestsWithTokenRedacted(t *testing.T) {
	srv := newAuthServer(t)

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--token", "secret-token", "--api-host", srv.URL, "--debug", "incidents", "get", "inc-1")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "GET /v1/incidents/inc-1") || !strings.Contains(stderr, "DB down") {
		t.Errorf("--debug should dump the request and response to stderr, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "secret-token") || !strings.Contains(stderr, "[REDACTED]") {
		t.Errorf("--debug must redact the Authorization header, got:\n%s", stderr)
	}
}

func TestAPIHostFlagOverridesLoginHost(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds("http://127.0.0.1:1", time.Now().Add(time.Hour)))

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--api-host", srv.URL, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "Bearer login-at" {
		t.Errorf("Authorization = %q, err = %v; want the login sent to --api-host", srv.lastHeader(), err)
	}
}

func TestAPIHostEnvOverridesLoginHost(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds("http://127.0.0.1:1", time.Now().Add(time.Hour)))
	t.Setenv("FH_API_HOST", srv.URL)

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "Bearer login-at" {
		t.Errorf("Authorization = %q, err = %v; want the login sent to FH_API_HOST", srv.lastHeader(), err)
	}
}

func TestExpiredLoginRefreshesBeforeCall(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(-time.Minute)))

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "Bearer refreshed-at" {
		t.Errorf("Authorization = %q, err = %v; want the refreshed token", srv.lastHeader(), err)
	}
	stored, _ := auth.DefaultStore().Load("us")
	if stored.RefreshToken != "refreshed-rt" {
		t.Errorf("rotated refresh token not saved: %q", stored.RefreshToken)
	}
}

func TestLoginExpiredIsReported(t *testing.T) {
	srv := newAuthServer(t)
	srv.refreshInvalid = true
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(-time.Minute)))

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "incidents", "get", "inc-1")

	if err == nil || !strings.Contains(stderr, "login expired") || !strings.Contains(stderr, "fhcli login") {
		t.Errorf("err = %v, stderr = %q; want a login-expired error", err, stderr)
	}
	if len(srv.headers) != 0 {
		t.Error("no API request should be sent with an expired login")
	}
}

func TestExpiredLoginDoesNotFailEvent(t *testing.T) {
	srv := newAuthServer(t)
	srv.refreshInvalid = true
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(-time.Minute)))

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "event", "deployed")

	if err != nil {
		t.Errorf("event must exit 0 by default even when the login is expired, got %v", err)
	}
}

func TestCorruptLoginIsReported(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(auth.ConfigDir(), "credentials")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "us.json"), []byte("garbage"), 0o600)

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "incidents", "get", "inc-1")

	if err == nil || !strings.Contains(stderr, "fhcli login") {
		t.Errorf("err = %v, stderr = %q; want an error pointing at fhcli login", err, stderr)
	}
}

func TestNoCredentialsMentionsLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "incidents", "get", "inc-1")

	if err == nil || !strings.Contains(stderr, "API token is required") || !strings.Contains(stderr, "fhcli login") {
		t.Errorf("err = %v, stderr = %q", err, stderr)
	}
}

func TestUnauthorizedAddsLoginHint(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"The bearer token you provided is invalid or expired."}`))
	}))
	defer ts.Close()

	for _, args := range [][]string{
		{"incidents", "get", "inc-1"},                 // SDK client
		{"api", "incidents", "get-incident", "inc-1"}, // raw client
	} {
		_, stderr, err := runCmdFull(t, ts.URL, args...)
		if err == nil || !strings.Contains(stderr, "fhcli login") {
			t.Errorf("%v: stderr = %q; want the fhcli login hint on a 401", args, stderr)
		}
	}
}

func TestOtherErrorsHaveNoLoginHint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"not found"}`))
	}))
	defer ts.Close()

	_, stderr, _ := runCmdFull(t, ts.URL, "api", "incidents", "get-incident", "inc-1")

	if strings.Contains(stderr, "fhcli login") {
		t.Errorf("a 404 should not suggest logging in: %q", stderr)
	}
}

func TestConfigAPIHostDoesNotRedirectLogin(t *testing.T) {
	srv := newAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))
	cfg := filepath.Join(t.TempDir(), "firehydrant.cfg")
	os.WriteFile(cfg, []byte("apiHost: http://127.0.0.1:1\n"), 0o600)
	t.Setenv("FH_API_HOST", "")
	os.Unsetenv("FH_API_HOST")

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--config", cfg, "incidents", "get", "inc-1")

	if err != nil || srv.lastHeader() != "Bearer login-at" {
		t.Errorf("Authorization = %q, err = %v, stderr = %s; a config-file apiHost must not redirect the login's token", srv.lastHeader(), err, stderr)
	}
}

func TestInitWritesBareDefaultHostLikeV0015(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "firehydrant.cfg")

	if _, err := runCmdNoToken(t, "--config", cfg, "init", "--environment", "production"); err != nil {
		t.Fatal(err)
	}

	// Older fhcli versions reading this file fail on an empty apiHost.
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), "apiHost: api.firehydrant.io\n") {
		t.Errorf("init should write the bare default host, as v0.0.15 did:\n%s", data)
	}
}

func TestExecuteReturnsCommandFailureWithExpiredLogin(t *testing.T) {
	srv := newAuthServer(t)
	srv.refreshInvalid = true
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(-time.Minute)))

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "--ignore-errors=false", "execute", "false")

	if err == nil {
		t.Error("a failing wrapped command must fail execute with --ignore-errors=false")
	}
}

func TestDryRunShowsLoginHost(t *testing.T) {
	storeLogin(t, loginCreds("https://api.eu.example", time.Now().Add(time.Hour)))

	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "api", "incidents", "get-incident", "inc-1", "--dry-run")

	if err != nil || !strings.Contains(stdout, `"url":"https://api.eu.example/v1/incidents/inc-1"`) {
		t.Errorf("stdout = %s, err = %v; dry-run must show the host the request goes to", stdout, err)
	}
}
