package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	firehydrantgosdk "github.com/firehydrant/firehydrant-go-sdk"
	"github.com/firehydrant/firehydrant-go-sdk/models/components"
	urfcli "github.com/urfave/cli/v3"
)

// newTestServer returns an httptest.Server that routes requests to a handler map.
// The map key is "METHOD /path" (e.g. "GET /v1/incidents").
// Unmatched routes return 404.
func newTestServer(t *testing.T, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		// Try prefix matching for parameterized paths
		for pattern, h := range routes {
			if matchRoute(pattern, r.Method, r.URL.Path) {
				h(w, r)
				return
			}
		}
		t.Logf("unmatched request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
}

// matchRoute does simple prefix-based matching for routes with path params.
func matchRoute(pattern, method, path string) bool {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) != 2 || parts[0] != method {
		return false
	}
	// Check if the pattern path is a prefix of the actual path
	// This handles cases like "GET /v1/incidents/" matching "/v1/incidents/abc123"
	if strings.HasSuffix(parts[1], "/") && strings.HasPrefix(path, parts[1]) {
		return true
	}
	return false
}

func jsonHandler(statusCode int, body any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(body)
	}
}

// runCmd builds a root command and runs it with the given args, capturing stdout.
// It prepends the --token and --api-host flags pointing at the test server.
func runCmd(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()
	out, _, err := runCmdFull(t, serverURL, args...)
	return out, err
}

// runCmdFull is like runCmd but also returns what the command wrote to stderr.
func runCmdFull(t *testing.T, serverURL string, args ...string) (string, string, error) {
	t.Helper()
	argv := append([]string{"fhcli", "--token", "test-token", "--api-host", serverURL}, args...)
	return runRoot(t, NewRootCmd("test", "0.0.0-test"), argv...)
}

// runRoot runs a built root command with argv (argv[0] is the program name),
// capturing stdout and stderr. The pipes are drained concurrently so large
// output can't fill a pipe buffer and deadlock the command.
func runRoot(t *testing.T, root *urfcli.Command, argv ...string) (string, string, error) {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr

	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { io.Copy(&outBuf, rOut); done <- struct{}{} }()
	go func() { io.Copy(&errBuf, rErr); done <- struct{}{} }()

	err := root.Run(context.Background(), argv)

	wOut.Close()
	wErr.Close()
	<-done
	<-done
	os.Stdout, os.Stderr = oldStdout, oldStderr
	rOut.Close()
	rErr.Close()

	return outBuf.String(), errBuf.String(), err
}

// runCmdNoToken runs a command without the --token flag (for init, install).
func runCmdNoToken(t *testing.T, args ...string) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	cmd := NewRootCmd("test", "0.0.0-test")
	fullArgs := append([]string{"fhcli"}, args...)

	err := cmd.Run(context.Background(), fullArgs)

	w.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	io.Copy(io.Discard, rErr)
	rErr.Close()

	return buf.String(), err
}

// ---------------------------------------------------------------------------
// Command registration tests
// ---------------------------------------------------------------------------

func TestCommandTree(t *testing.T) {
	cmd := NewRootCmd("test", "0.0.0-test")

	// Collect top-level command names
	topLevel := map[string]bool{}
	for _, c := range cmd.Commands {
		topLevel[c.Name] = true
	}

	expected := []string{"init", "event", "execute", "incidents", "services", "teams", "install", "api", "search", "spec", "login", "logout", "whoami"}
	for _, name := range expected {
		if !topLevel[name] {
			t.Errorf("expected top-level command %q to be registered", name)
		}
	}
}

func TestIncidentSubcommands(t *testing.T) {
	cmd := NewRootCmd("test", "0.0.0-test")

	var incidentsCmd *urfcli.Command
	for _, c := range cmd.Commands {
		if c.Name == "incidents" {
			incidentsCmd = c
			break
		}
	}
	if incidentsCmd == nil {
		t.Fatal("incidents command not found")
	}

	subcommands := map[string]bool{}
	for _, c := range incidentsCmd.Commands {
		subcommands[c.Name] = true
	}

	expected := []string{"list", "get", "note", "chat", "alerts", "summary", "events", "transcript", "update", "resolve", "triage", "context"}
	for _, name := range expected {
		if !subcommands[name] {
			t.Errorf("expected incidents subcommand %q to be registered", name)
		}
	}
}

func TestServicesSubcommands(t *testing.T) {
	cmd := NewRootCmd("test", "0.0.0-test")

	var servicesCmd *urfcli.Command
	for _, c := range cmd.Commands {
		if c.Name == "services" {
			servicesCmd = c
			break
		}
	}
	if servicesCmd == nil {
		t.Fatal("services command not found")
	}

	subcommands := map[string]bool{}
	for _, c := range servicesCmd.Commands {
		subcommands[c.Name] = true
	}

	expected := []string{"list", "get"}
	for _, name := range expected {
		if !subcommands[name] {
			t.Errorf("expected services subcommand %q to be registered", name)
		}
	}
}

func TestTeamsSubcommands(t *testing.T) {
	cmd := NewRootCmd("test", "0.0.0-test")

	var teamsCmd *urfcli.Command
	for _, c := range cmd.Commands {
		if c.Name == "teams" {
			teamsCmd = c
			break
		}
	}
	if teamsCmd == nil {
		t.Fatal("teams command not found")
	}

	subcommands := map[string]bool{}
	for _, c := range teamsCmd.Commands {
		subcommands[c.Name] = true
	}

	expected := []string{"list", "get", "oncall"}
	for _, name := range expected {
		if !subcommands[name] {
			t.Errorf("expected teams subcommand %q to be registered", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Global flag tests (backwards compatibility)
// ---------------------------------------------------------------------------

func TestGlobalFlags(t *testing.T) {
	cmd := NewRootCmd("test", "0.0.0-test")

	flagNames := map[string]bool{}
	for _, f := range cmd.Flags {
		for _, name := range f.Names() {
			flagNames[name] = true
		}
	}

	// These flags must exist for backwards compatibility
	expectedFlags := []string{
		"token", "apiKey", // auth token + legacy alias
		"api-host", "apiHost", // api host + legacy alias
		"debug",            // debug mode
		"interactive", "i", // interactive output
		"ignore-errors", "ignoreErrors", // ignore errors + legacy alias
		"config", // config file
	}

	for _, name := range expectedFlags {
		if !flagNames[name] {
			t.Errorf("expected global flag %q to be registered (backwards compatibility)", name)
		}
	}
}

func TestSharedFlags(t *testing.T) {
	cmd := NewRootCmd("test", "0.0.0-test")

	// Find the event command and check its flags
	var eventCommand *urfcli.Command
	for _, c := range cmd.Commands {
		if c.Name == "event" {
			eventCommand = c
			break
		}
	}
	if eventCommand == nil {
		t.Fatal("event command not found")
	}

	flagNames := map[string]bool{}
	for _, f := range eventCommand.Flags {
		for _, name := range f.Names() {
			flagNames[name] = true
		}
	}

	expectedFlags := []string{"identities", "labels", "environment", "service"}
	for _, name := range expectedFlags {
		if !flagNames[name] {
			t.Errorf("expected shared flag %q on event command (backwards compatibility)", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Legacy command tests: event
// ---------------------------------------------------------------------------

func TestEventCmd(t *testing.T) {
	var receivedBody map[string]any
	var receivedMethod string

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/changes/events": func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated) // CreateChangeEvent responds 201
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "evt-123",
				"summary": "deployed payment service",
			})
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "event", "deployed", "payment", "service",
		"--environment", "production",
		"--service", "payment-svc",
		"--labels", "executor=jenkins",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("expected POST, got %s", receivedMethod)
	}

	if receivedBody == nil {
		t.Fatal("no request body received")
	}

	if summary, ok := receivedBody["summary"].(string); !ok || summary != "deployed payment service" {
		t.Errorf("expected summary 'deployed payment service', got %v", receivedBody["summary"])
	}

	if !strings.Contains(out, "evt-123") {
		t.Errorf("expected output to contain event ID, got: %s", out)
	}
}

func TestEventCmdWithIdentities(t *testing.T) {
	var receivedBody map[string]any

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/changes/events": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated) // CreateChangeEvent responds 201
			json.NewEncoder(w).Encode(map[string]any{"id": "evt-456"})
		},
	})
	defer ts.Close()

	_, err := runCmd(t, ts.URL, "event", "deploy",
		"--identities", "image=quay.io/app:abc123",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody == nil {
		t.Fatal("no request body received")
	}

	// Verify change_identities were sent
	identities, ok := receivedBody["change_identities"].([]any)
	if !ok || len(identities) == 0 {
		t.Errorf("expected change_identities in request body, got: %v", receivedBody["change_identities"])
	}
}

// ---------------------------------------------------------------------------
// Legacy command tests: execute
// ---------------------------------------------------------------------------

func TestExecuteCmd(t *testing.T) {
	var receivedBody map[string]any

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/changes/events": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated) // CreateChangeEvent responds 201
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "evt-exec-123",
				"summary": "echo hello",
			})
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "execute", "echo", "hello",
		"--environment", "staging",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody == nil {
		t.Fatal("no request body received")
	}

	if !strings.Contains(out, "evt-exec-123") {
		t.Errorf("expected output to contain event ID, got: %s", out)
	}
}

func TestExecuteCmdRequiresArgs(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "execute")
	// As in v0.0.15: a missing command is a usage error, so it exits
	// non-zero regardless of --ignore-errors.
	if err == nil {
		t.Error("expected a non-zero exit when no command is passed")
	}
	if !strings.Contains(stderr, "No command passed") {
		t.Errorf("expected 'No command passed' on stderr, got: %s", stderr)
	}
}

// ---------------------------------------------------------------------------
// Legacy command tests: init
// ---------------------------------------------------------------------------

func TestInitCmd(t *testing.T) {
	tmpFile := t.TempDir() + "/test-config.cfg"

	out, err := runCmdNoToken(t, "--config", tmpFile, "init",
		"--environment", "production",
		"--service", "web",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, tmpFile) {
		t.Errorf("expected output to contain config path %s, got: %s", tmpFile, out)
	}

	// Verify the config file was written
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "production") {
		t.Errorf("expected config to contain 'production', got: %s", content)
	}
	if !strings.Contains(content, "web") {
		t.Errorf("expected config to contain 'web', got: %s", content)
	}
}

// ---------------------------------------------------------------------------
// Incidents command tests
// ---------------------------------------------------------------------------

func TestIncidentsListCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents": jsonHandler(200, map[string]any{
			"data": []map[string]any{
				{"id": "inc-1", "name": "Test Incident"},
			},
		}),
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "inc-1") {
		t.Errorf("expected output to contain incident ID, got: %s", out)
	}
}

func TestIncidentsListCmdWithFilters(t *testing.T) {
	var receivedQuery string

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents": func(w http.ResponseWriter, r *http.Request) {
			receivedQuery = r.URL.RawQuery
			jsonHandler(200, map[string]any{"data": []any{}})(w, r)
		},
	})
	defer ts.Close()

	_, err := runCmd(t, ts.URL, "incidents", "list",
		"--status", "active",
		"--severity", "SEV1",
		"--query", "payment",
		"--page", "2",
		"--per-page", "10",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(receivedQuery, "status=active") {
		t.Errorf("expected query to contain status=active, got: %s", receivedQuery)
	}
	if !strings.Contains(receivedQuery, "payment") {
		t.Errorf("expected query to contain payment, got: %s", receivedQuery)
	}
}

func TestIncidentsGetCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":   "inc-abc",
				"name": "Database outage",
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "get", "inc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "inc-abc") {
		t.Errorf("expected output to contain incident ID, got: %s", out)
	}
}

func TestIncidentsGetCmdRequiresID(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "get")
	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "incident ID is required") {
		t.Errorf("expected 'incident ID is required' on stderr, got: %s", stderr)
	}
}

func TestIncidentsNoteCmd(t *testing.T) {
	var receivedBody map[string]any

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			jsonHandler(201, map[string]any{"id": "note-1"})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "note", "inc-abc", "--body", "Found the root cause")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody == nil {
		t.Fatal("no request body received")
	}
	if body, ok := receivedBody["body"].(string); !ok || body != "Found the root cause" {
		t.Errorf("expected body 'Found the root cause', got: %v", receivedBody["body"])
	}
	if !strings.Contains(out, "note-1") {
		t.Errorf("expected output to contain note ID, got: %s", out)
	}
}

func TestIncidentsCommandsResolveIncidentNumbers(t *testing.T) {
	var notePath string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/incidents/623", jsonHandler(200, map[string]any{"id": "inc-uuid", "number": 623}))
	mux.HandleFunc("POST /v1/incidents/{id}/notes", func(w http.ResponseWriter, r *http.Request) {
		notePath = r.URL.Path
		jsonHandler(201, map[string]any{"id": "note-1"})(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	if _, err := runCmd(t, ts.URL, "incidents", "note", "#623", "--body", "hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notePath != "/v1/incidents/inc-uuid/notes" {
		t.Errorf("note posted to %q; the incident number should be resolved to its ID first", notePath)
	}
}

func TestIncidentsChatCmd(t *testing.T) {
	var receivedBody map[string]any

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"POST /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			jsonHandler(201, map[string]any{"id": "chat-1"})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "chat", "inc-abc", "--body", "Checking logs now")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if body, ok := receivedBody["body"].(string); !ok || body != "Checking logs now" {
		t.Errorf("expected body 'Checking logs now', got: %v", receivedBody["body"])
	}
	if !strings.Contains(out, "chat-1") {
		t.Errorf("expected output to contain chat ID, got: %s", out)
	}
}

func TestIncidentsAlertsCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			// Match alerts path
			if strings.HasSuffix(r.URL.Path, "/alerts") {
				jsonHandler(200, map[string]any{
					"data": []map[string]any{
						{"id": "alert-1", "summary": "High CPU"},
					},
				})(w, r)
				return
			}
			http.NotFound(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "alerts", "inc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alert-1") {
		t.Errorf("expected output to contain alert ID, got: %s", out)
	}
}

func TestIncidentsSummaryCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/audiences/summaries/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"data": map[string]any{
					"incident_id": "inc-abc",
					"summaries": []map[string]any{
						{"audience_slug": "internal", "summary": map[string]any{"content": "Database issues"}},
					},
				},
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "summary", "inc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Database issues") {
		t.Errorf("expected output to contain summary text, got: %s", out)
	}
}

func TestIncidentsEventsCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/events") {
				jsonHandler(200, map[string]any{
					"data": []map[string]any{
						{"id": "event-1", "type": "note"},
					},
				})(w, r)
				return
			}
			http.NotFound(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "events", "inc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "event-1") {
		t.Errorf("expected output to contain event ID, got: %s", out)
	}
}

func TestIncidentsTranscriptCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/transcript") {
				jsonHandler(200, map[string]any{
					"data": []map[string]any{
						{"id": "tx-1", "speaker": "Dylan", "words": "We're seeing errors"},
					},
					"pagination": map[string]any{"count": 1},
				})(w, r)
				return
			}
			http.NotFound(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "transcript", "inc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "tx-1") {
		t.Errorf("expected output to contain transcript ID, got: %s", out)
	}
}

func TestIncidentsUpdateCmd(t *testing.T) {
	var receivedBody map[string]any
	var receivedMethod string

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"PATCH /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			json.NewDecoder(r.Body).Decode(&receivedBody)
			jsonHandler(200, map[string]any{
				"id":       "inc-abc",
				"severity": "SEV1",
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "update", "inc-abc",
		"--severity", "SEV1",
		"--summary", "DB connection pool exhaustion",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedMethod != "PATCH" {
		t.Errorf("expected PATCH, got %s", receivedMethod)
	}
	if !strings.Contains(out, "inc-abc") {
		t.Errorf("expected output to contain incident ID, got: %s", out)
	}
}

func TestIncidentsResolveCmd(t *testing.T) {
	var receivedMethod string

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"PUT /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			jsonHandler(200, map[string]any{
				"id":     "inc-abc",
				"active": false,
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "resolve", "inc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedMethod != "PUT" {
		t.Errorf("expected PUT, got %s", receivedMethod)
	}
	if !strings.Contains(out, "inc-abc") {
		t.Errorf("expected output to contain incident ID, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Services command tests
// ---------------------------------------------------------------------------

func TestServicesListCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/services": jsonHandler(200, map[string]any{
			"data": []map[string]any{
				{"id": "svc-1", "name": "payment-service"},
			},
		}),
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "services", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "svc-1") {
		t.Errorf("expected output to contain service ID, got: %s", out)
	}
}

func TestServicesGetCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/services/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":   "svc-abc",
				"name": "payment-service",
				"links": []map[string]any{
					{"name": "Datadog", "href_url": "https://datadog.com/dash"},
				},
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "services", "get", "svc-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "svc-abc") {
		t.Errorf("expected output to contain service ID, got: %s", out)
	}
}

func TestServicesGetCmdRequiresID(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "services", "get")
	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "service ID is required") {
		t.Errorf("expected 'service ID is required' on stderr, got: %s", stderr)
	}
}

// ---------------------------------------------------------------------------
// Teams command tests
// ---------------------------------------------------------------------------

func TestTeamsListCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/teams": jsonHandler(200, map[string]any{
			"data": []map[string]any{
				{"id": "team-1", "name": "Platform"},
			},
		}),
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "teams", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "team-1") {
		t.Errorf("expected output to contain team ID, got: %s", out)
	}
}

func TestTeamsGetCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/teams/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":   "team-abc",
				"name": "Platform",
				"slack_channel": map[string]any{
					"name": "#platform-eng",
				},
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "teams", "get", "team-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "team-abc") {
		t.Errorf("expected output to contain team ID, got: %s", out)
	}
}

func TestTeamsGetCmdRequiresID(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "teams", "get")
	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "team ID is required") {
		t.Errorf("expected 'team ID is required' on stderr, got: %s", stderr)
	}
}

func TestTeamsOnCallCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/signals_on_call": jsonHandler(200, map[string]any{
			"data": []map[string]any{
				{"id": "sched-1", "name": "Primary"},
			},
		}),
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "teams", "oncall")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "sched-1") {
		t.Errorf("expected output to contain schedule ID, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Install command tests
// ---------------------------------------------------------------------------

func TestInstallSkillsCmd(t *testing.T) {
	out, err := runCmdNoToken(t, "install", "--skills")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "FireHydrant") {
		t.Errorf("expected skill file output to contain 'FireHydrant', got: %s", out[:min(len(out), 100)])
	}
}

// ---------------------------------------------------------------------------
// New helper command tests: triage
// ---------------------------------------------------------------------------

func TestIncidentsTriageCmd(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			// Distinguish between get incident and alerts/resolve etc
			path := r.URL.Path
			if strings.HasSuffix(path, "/alerts") {
				jsonHandler(200, map[string]any{
					"data": []map[string]any{
						{"id": "alert-1", "summary": "High CPU"},
					},
				})(w, r)
				return
			}
			// Plain incident get
			jsonHandler(200, map[string]any{
				"id":   "inc-triage",
				"name": "Test triage incident",
			})(w, r)
		},
		"GET /v1/audiences/summaries/": jsonHandler(200, map[string]any{
			"data": map[string]any{
				"incident_id": "inc-triage",
				"summaries": []map[string]any{
					{"audience_slug": "internal", "summary": map[string]any{"content": "Everything is on fire"}},
				},
			},
		}),
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "triage", "inc-triage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Everything is on fire") {
		t.Errorf("expected triage output to contain summary content, got: %s", out)
	}

	// Parse the output JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse output JSON: %v\noutput: %s", err, out)
	}

	// Verify all three keys are present
	if result["incident"] == nil {
		t.Error("expected 'incident' key in triage result")
	}
	if result["alerts"] == nil {
		t.Error("expected 'alerts' key in triage result")
	}
	if result["summaries"] == nil {
		t.Error("expected 'summaries' key in triage result")
	}
}

func TestIncidentsTriageCmdRequiresID(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "triage")
	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "incident ID is required") {
		t.Errorf("expected 'incident ID is required' on stderr, got: %s", stderr)
	}
}

// ---------------------------------------------------------------------------
// New helper command tests: context
// ---------------------------------------------------------------------------

func TestIncidentsContextCmd(t *testing.T) {
	svcID := "svc-pay"
	teamID := "team-plat"

	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":   "inc-ctx",
				"name": "Context test incident",
				"services": []map[string]any{
					{"id": svcID, "name": "payment-service"},
				},
			})(w, r)
		},
		"GET /v1/services/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":   svcID,
				"name": "payment-service",
				"links": []map[string]any{
					{"name": "Datadog", "href_url": "https://datadog.com/dashboard"},
					{"name": "Grafana", "href_url": "https://grafana.com/dashboard"},
				},
				"owner": map[string]any{
					"id":   teamID,
					"name": "Platform",
				},
			})(w, r)
		},
		"GET /v1/teams/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":   teamID,
				"name": "Platform",
				"slack_channel": map[string]any{
					"name": "#platform-eng",
				},
			})(w, r)
		},
		"GET /v1/signals_on_call": jsonHandler(200, map[string]any{
			"data": []map[string]any{
				{"id": "sched-1", "name": "Primary On-Call"},
			},
		}),
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "context", "inc-ctx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse output JSON: %v\noutput: %s", err, out)
	}

	// Verify top-level keys
	if result["incident"] == nil {
		t.Error("expected 'incident' key in context result")
	}

	services, ok := result["services"].([]any)
	if !ok {
		t.Fatal("expected 'services' to be an array")
	}
	if len(services) == 0 {
		t.Fatal("expected at least one service in context result")
	}

	// Verify enriched service has dashboard_links
	svc := services[0].(map[string]any)
	if svc["service"] == nil {
		t.Error("expected 'service' key in enriched service")
	}
	links, ok := svc["dashboard_links"].([]any)
	if !ok {
		t.Fatal("expected 'dashboard_links' to be an array")
	}
	if len(links) != 2 {
		t.Errorf("expected 2 dashboard links, got %d", len(links))
	}

	teams, ok := result["teams"].([]any)
	if !ok {
		t.Fatal("expected 'teams' to be an array")
	}
	if len(teams) == 0 {
		t.Fatal("expected at least one team in context result")
	}

	// Verify enriched team has on_call_schedules
	team := teams[0].(map[string]any)
	if team["team"] == nil {
		t.Error("expected 'team' key in enriched team")
	}
	if team["on_call_schedules"] == nil {
		t.Error("expected 'on_call_schedules' key in enriched team")
	}
}

func TestIncidentsContextCmdRequiresID(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "context")
	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "incident ID is required") {
		t.Errorf("expected 'incident ID is required' on stderr, got: %s", stderr)
	}
}

func TestIncidentsContextCmdNoServices(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			jsonHandler(200, map[string]any{
				"id":       "inc-empty",
				"name":     "No services incident",
				"services": []map[string]any{},
			})(w, r)
		},
	})
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "incidents", "context", "inc-empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse output JSON: %v\noutput: %s", err, out)
	}

	// Should have empty arrays, not null
	services, ok := result["services"].([]any)
	if !ok {
		t.Fatal("expected 'services' to be an array (not null)")
	}
	if len(services) != 0 {
		t.Errorf("expected empty services array, got %d items", len(services))
	}

	teams, ok := result["teams"].([]any)
	if !ok {
		t.Fatal("expected 'teams' to be an array (not null)")
	}
	if len(teams) != 0 {
		t.Errorf("expected empty teams array, got %d items", len(teams))
	}
}

// ---------------------------------------------------------------------------
// SDK client creation test
// ---------------------------------------------------------------------------

func TestGetClientWithoutToken(t *testing.T) {
	_, err := getClient(context.Background())
	if err == nil {
		t.Error("expected error when no client in context")
	}
	if !strings.Contains(err.Error(), "API token is required") {
		t.Errorf("expected 'API token is required' error, got: %v", err)
	}
}

func TestGetClientWithToken(t *testing.T) {
	client := firehydrantgosdk.New(
		firehydrantgosdk.WithSecurity(components.Security{APIKey: "test"}),
	)
	ctx := context.WithValue(context.Background(), sdkClientKey, client)
	c, err := getClient(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Error("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// Utility function tests
// ---------------------------------------------------------------------------

func TestParseKV(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{"", map[string]string{}},
		{"k=v", map[string]string{"k": "v"}},
		{"a=1,b=2", map[string]string{"a": "1", "b": "2"}},
		{"image=quay.io/app:abc", map[string]string{"image": "quay.io/app:abc"}},
	}

	for _, tt := range tests {
		result := parseKV(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseKV(%q): expected %d keys, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for k, v := range tt.expected {
			if result[k] != v {
				t.Errorf("parseKV(%q): expected %s=%s, got %s=%s", tt.input, k, v, k, result[k])
			}
		}
	}
}

func TestParamToList(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
	}

	for _, tt := range tests {
		result := paramToList(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("paramToList(%q): expected %d items, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for i, v := range tt.expected {
			if result[i] != v {
				t.Errorf("paramToList(%q)[%d]: expected %q, got %q", tt.input, i, v, result[i])
			}
		}
	}
}

func TestIncidentsGetCmdAPIErrorExitsNonZero(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": jsonHandler(500, map[string]any{"detail": "boom"}),
	})
	defer ts.Close()

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "get", "inc-abc")
	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "error") {
		t.Errorf("expected a JSON error on stderr, got: %s", stderr)
	}
}

func TestIncidentsTranscriptCmdForwardsSort(t *testing.T) {
	var gotSort string
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"GET /v1/incidents/": func(w http.ResponseWriter, r *http.Request) {
			gotSort = r.URL.Query().Get("sort")
			jsonHandler(200, map[string]any{"data": []any{}})(w, r)
		},
	})
	defer ts.Close()

	if _, err := runCmd(t, ts.URL, "incidents", "transcript", "inc-abc", "--sort", "desc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSort != "desc" {
		t.Errorf("expected sort=desc to be forwarded, got: %q", gotSort)
	}
}
