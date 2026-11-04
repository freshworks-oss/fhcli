package cli

// Behaviour of the CI commands (event, execute, init) as released in
// v0.0.15. Existing pipelines depend on it, so these tests pin it.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// changeServer records each change-event body; a summary containing "fail"
// gets a 500.
func changeServer(t *testing.T) (string, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/changes/events": func(w http.ResponseWriter, r *http.Request) {
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			bodies = append(bodies, b)
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(b["summary"].(string), "fail") {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"detail":"boom"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"chg-1"}`))
		},
	})
	t.Cleanup(ts.Close)
	return ts.URL, &bodies
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "firehydrant.cfg")
	os.WriteFile(path, []byte(yaml), 0o600)
	t.Setenv("FH_SERVICE", "")
	t.Setenv("FH_ENVIRONMENT", "")
	t.Setenv("FH_LABELS", "")
	t.Setenv("FH_IDENTITIES", "")
	for _, k := range []string{"FH_SERVICE", "FH_ENVIRONMENT", "FH_LABELS", "FH_IDENTITIES"} {
		os.Unsetenv(k)
	}
	return path
}

func TestEventPrintsTextLikeV0015(t *testing.T) {
	url, _ := changeServer(t)

	stdout, _, err := runCmdFull(t, url, "event", "Deployed", "v1")

	if err != nil || stdout != "Created change event chg-1\n" {
		t.Errorf("stdout = %q, err = %v", stdout, err)
	}
}

func TestEventAppliesConfigFileSharedFlags(t *testing.T) {
	url, bodies := changeServer(t)
	cfg := writeConfig(t, "service: cfg-svc\nenvironment: cfg-env\nlabels: team=db\nidentities: image=x:1\n")

	_, stderr, err := runCmdFull(t, url, "--config", cfg, "event", "Deployed")

	if err != nil || len(*bodies) != 1 {
		t.Fatalf("err = %v, stderr = %s, bodies = %v", err, stderr, *bodies)
	}
	b := (*bodies)[0]
	if s, _ := json.Marshal([]any{b["services"], b["environments"], b["labels"], b["change_identities"]}); string(s) != `[["cfg-svc"],["cfg-env"],{"team":"db"},[{"type":"image","value":"x:1"}]]` {
		t.Errorf("config values not applied: %s", s)
	}
}

func TestEventFlagBeatsConfigFile(t *testing.T) {
	url, bodies := changeServer(t)
	cfg := writeConfig(t, "service: cfg-svc\n")

	runCmdFull(t, url, "--config", cfg, "event", "--service", "flag-svc", "Deployed")

	if len(*bodies) != 1 || (*bodies)[0]["services"].([]any)[0] != "flag-svc" {
		t.Errorf("bodies = %v; a flag must win over the config file", *bodies)
	}
}

func TestConfigFileIgnoreErrorsFalse(t *testing.T) {
	url, _ := changeServer(t)
	cfg := writeConfig(t, "ignoreErrors: false\n")

	_, _, err := runCmdFull(t, url, "--config", cfg, "event", "please", "fail")

	if err == nil {
		t.Error("ignoreErrors: false in the config file must make API errors exit non-zero")
	}
}

func TestAPIErrorPrintsTextAndExitsZeroByDefault(t *testing.T) {
	url, _ := changeServer(t)

	stdout, _, err := runCmdFull(t, url, "event", "please", "fail")

	if err != nil {
		t.Fatalf("default ignoreErrors must exit 0, got %v", err)
	}
	if !strings.Contains(stdout, "500") || !strings.HasSuffix(stdout, "ignoreErrors is set so exit code will be 0\n") {
		t.Errorf("stdout = %q, want the error and the ignoreErrors notice", stdout)
	}
}

func TestEventWithoutCredentialsExitsNonZero(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "event", "Deployed")

	if err == nil || !strings.Contains(stderr, "API token is required") {
		t.Errorf("err = %v, stderr = %q; missing credentials are a config error, not an API error", err, stderr)
	}
}

func TestExecuteWithoutCredentialsDoesNotRunTheCommand(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	marker := filepath.Join(t.TempDir(), "ran")

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "execute", "touch", marker)

	if err == nil {
		t.Error("execute without credentials must exit non-zero")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("execute without credentials must not run the command")
	}
}

func TestExecuteWithoutCommandExitsNonZero(t *testing.T) {
	url, _ := changeServer(t)

	_, _, err := runCmdFull(t, url, "execute")

	if err == nil {
		t.Error("execute with no command must exit non-zero")
	}
}

func TestExecutePrintsProgressLikeV0015(t *testing.T) {
	url, _ := changeServer(t)

	stdout, _, err := runCmdFull(t, url, "execute", "--", "sh", "-c", "exit 0")

	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Executing command sh with args -c exit 0\n", "Executed command, duration ", "Created change event chg-1\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
	}
}

func TestInitPrintsTextLikeV0015(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.cfg")

	out, err := runCmdNoToken(t, "--config", path, "init")

	if err != nil || out != "Wrote config file "+path+"\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

// A stored login that can no longer refresh is new in this version, so it
// keeps the forgiving behaviour: the wrapped command still runs.
func TestExecuteWithExpiredLoginStillRunsTheCommand(t *testing.T) {
	srv := newAuthServer(t)
	srv.refreshInvalid = true
	storeLogin(t, loginCreds(srv.URL, time.Now().Add(-time.Minute)))
	marker := filepath.Join(t.TempDir(), "ran")

	_, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "execute", "touch", marker)

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Error("an expired login must not stop the deploy command from running")
	}
	if err != nil {
		t.Errorf("default ignoreErrors should exit 0, got %v", err)
	}
}
