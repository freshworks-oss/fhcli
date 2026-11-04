package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/firehydrant/fhcli/pkg/auth"
)

// oauthServer is a minimal Doorkeeper stand-in for command-level tests.
type oauthServer struct {
	*httptest.Server
	challenge string
	revoked   []url.Values
}

func newOAuthServer(t *testing.T) *oauthServer {
	t.Helper()
	s := &oauthServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		s.challenge = q.Get("code_challenge")
		back, _ := url.Parse(q.Get("redirect_uri"))
		back.RawQuery = url.Values{"code": {"the-code"}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != s.challenge {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new-at","refresh_token":"new-rt","token_type":"Bearer","expires_in":7200}`))
	})
	mux.HandleFunc("POST /oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		s.revoked = append(s.revoked, r.PostForm)
		w.Write([]byte("{}"))
	})
	mux.HandleFunc("GET /v1/current_user", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && r.Header.Get("Authorization") != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": "Dylan", "email": "dylan@example.com", "organization_id": "org-1", "organization_name": "FireHydrant"})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// useFakeLogin points login at srv, a free port, and a simulated browser.
func useFakeLogin(t *testing.T, srv *oauthServer) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	oldLookup, oldPorts, oldOpen, oldTimeout := lookupRegion, loginPorts, openBrowser, loginTimeout
	t.Cleanup(func() { lookupRegion, loginPorts, openBrowser, loginTimeout = oldLookup, oldPorts, oldOpen, oldTimeout })
	lookupRegion = func(name, clientID string) (auth.Region, error) {
		if _, err := auth.LookupRegion(name, "x"); err != nil {
			return auth.Region{}, err
		}
		return auth.Region{Name: name, AppHost: srv.URL, APIHost: srv.URL, ClientID: "cid"}, nil
	}
	loginPorts = []int{port}
	openBrowser = func(u string) error {
		go func() {
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginCommandStoresLogin(t *testing.T) {
	srv := newOAuthServer(t)
	useFakeLogin(t, srv)

	stdout, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "login", "--region", "eu")

	if err != nil {
		t.Fatalf("err = %v, stderr = %s", err, stderr)
	}
	var out map[string]string
	json.Unmarshal([]byte(stdout), &out)
	if out["user"] != "Dylan" || out["organization"] != "FireHydrant" || out["organization_id"] != "org-1" || out["region"] != "eu" || out["email"] != "dylan@example.com" {
		t.Errorf("stdout = %s", stdout)
	}
	if strings.Contains(stdout+stderr, "new-at") || strings.Contains(stdout+stderr, "new-rt") {
		t.Error("login output must not include tokens")
	}
	stored, err := auth.DefaultStore().Load("eu")
	if err != nil || stored.AccessToken != "new-at" {
		t.Errorf("stored = %+v, err = %v", stored, err)
	}
	if auth.CurrentRegion() != "eu" {
		t.Errorf("CurrentRegion = %q, want eu", auth.CurrentRegion())
	}
}

func TestLoginThenWhoami(t *testing.T) {
	srv := newOAuthServer(t)
	useFakeLogin(t, srv)
	if _, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "login"); err != nil {
		t.Fatalf("login: %v %s", err, stderr)
	}

	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "whoami")

	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	json.Unmarshal([]byte(stdout), &out)
	if out["source"] != "login" || out["region"] != "us" || out["api_host"] != srv.URL || out["organization"] != "FireHydrant" || out["expires_at"] == "" {
		t.Errorf("whoami = %s", stdout)
	}
	if strings.Contains(stdout, "new-at") || strings.Contains(stdout, "new-rt") {
		t.Error("whoami must not print tokens")
	}
}

func TestLoginNoBrowserPrintsURL(t *testing.T) {
	srv := newOAuthServer(t)
	useFakeLogin(t, srv)
	openBrowser = func(string) error { t.Error("--no-browser must not open a browser"); return nil }
	loginTimeout = 100 * time.Millisecond

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "login", "--no-browser")

	if err == nil || !strings.Contains(stderr, "timed out") {
		t.Errorf("err = %v, stderr = %s; want a timeout", err, stderr)
	}
	if !strings.Contains(stderr, srv.URL+"/oauth/authorize?") {
		t.Errorf("stderr = %s; want the authorize URL", stderr)
	}
}

func TestLoginUnknownRegion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "login", "--region", "mars")

	if err == nil || !strings.Contains(stderr, `unknown region \"mars\"`) {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

func TestLogoutRevokesAndDeletes(t *testing.T) {
	srv := newOAuthServer(t)
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(time.Hour)))

	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "logout")

	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != `{"logged_out":"us"}` {
		t.Errorf("stdout = %s", stdout)
	}
	if len(srv.revoked) != 2 || srv.revoked[0].Get("token") != "login-at" || srv.revoked[1].Get("token") != "login-rt" {
		t.Errorf("revoked = %v, want both tokens", srv.revoked)
	}
	if _, err := auth.DefaultStore().Load("us"); !errors.Is(err, auth.ErrNotLoggedIn) {
		t.Errorf("login still stored after logout: %v", err)
	}
}

func TestLogoutWhenNotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "logout")

	if err != nil || strings.TrimSpace(stdout) != `{"logged_out":"us"}` {
		t.Errorf("stdout = %s, err = %v", stdout, err)
	}
}

func TestWhoamiWithAPIToken(t *testing.T) {
	srv := newOAuthServer(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stdout, err := runCmd(t, srv.URL, "whoami")

	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	json.Unmarshal([]byte(stdout), &out)
	if out["source"] != "api_token" || out["user"] != "Dylan" || out["region"] != "" {
		t.Errorf("whoami = %s", stdout)
	}
	if strings.Contains(stdout, "test-token") {
		t.Error("whoami must not print the API token")
	}
}

func TestWhoamiNotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "whoami")

	if err == nil || !strings.Contains(stderr, "fhcli login") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}
