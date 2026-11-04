package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// runMini runs the CLI with the 7-operation fixture spec. An empty serverURL
// runs with no token and the default API host.
func runMini(t *testing.T, serverURL string, args ...string) (string, string, error) {
	t.Helper()
	argv := []string{"fhcli"}
	if serverURL != "" {
		argv = append(argv, "--token", "test-token", "--api-host", serverURL)
	}
	return runRoot(t, newRootCmd("test", "0.0.0-test", miniSpec(t), nil), append(argv, args...)...)
}

type dryRun struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Query  map[string][]string `json:"query"`
	Body   map[string]any      `json:"body"`
}

func parseDryRun(t *testing.T, out string) dryRun {
	t.Helper()
	var d dryRun
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, out)
	}
	return d
}

func failIfCalled(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be sent, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestAPIDryRunPrintsRequestWithoutSending(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident",
		"--name", "DB down", "--priority", "2", "--tag-list", "db", "--tag-list", "sev", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := parseDryRun(t, out)
	if d.Method != "POST" || d.URL != ts.URL+"/v1/incidents" {
		t.Errorf("request = %s %s", d.Method, d.URL)
	}
	want := map[string]any{"name": "DB down", "priority": float64(2), "tag_list": []any{"db", "sev"}}
	if !reflect.DeepEqual(d.Body, want) {
		t.Errorf("body = %v, want %v", d.Body, want)
	}
}

func TestAPIDataKeepsLargeIntegersExact(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident",
		"--data", `{"name": "DB down", "external_id": 12345678901234567, "big": 1000000000000000000000}`, "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "12345678901234567") || !strings.Contains(out, "1000000000000000000000") {
		t.Errorf("--data numbers were not sent as written:\n%s", out)
	}
}

func TestAPIDataRejectsTrailingJSON(t *testing.T) {
	ts := failIfCalled(t)

	_, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--data", `{"name": "a"} {"name": "b"}`, "--dry-run")

	if err == nil {
		t.Error("--data with two JSON objects should be rejected")
	}
}

func TestAPIDryRunIncludesQuery(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "list-incidents",
		"--page", "2", "--status", "open", "--tags", "a", "--tags", "b",
		"--custom-fields-field-id", "cf1", "--custom-fields-value", "v1", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := parseDryRun(t, out)
	// Array params use Rack array syntax (name[] / parent[][child]); Grape
	// rejects a repeated bare key and parses parent[child] as a Hash.
	want := map[string][]string{
		"page": {"2"}, "status": {"open"}, "tags[]": {"a", "b"},
		"custom_fields[][field_id]": {"cf1"}, "custom_fields[][value]": {"v1"},
	}
	if !reflect.DeepEqual(d.Query, want) {
		t.Errorf("query = %v, want %v", d.Query, want)
	}
	if d.Body != nil {
		t.Errorf("GET should have no body, got %v", d.Body)
	}
}

func TestAPISendsArrayQueryInRackSyntax(t *testing.T) {
	var got url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	}))
	defer ts.Close()

	_, _, err := runMini(t, ts.URL, "api", "incidents", "list-incidents",
		"--tags", "a", "--tags", "b", "--custom-fields-field-id", "f1", "--custom-fields-value", "v1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := url.Values{"tags[]": {"a", "b"}, "custom_fields[][field_id]": {"f1"}, "custom_fields[][value]": {"v1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("server received query %v, want %v", got, want)
	}
}

func TestAPIDryRunWorksWithoutToken(t *testing.T) {
	out, _, err := runMini(t, "", "api", "incidents", "list-incidents", "--dry-run")

	if err != nil {
		t.Fatalf("dry-run should not need a token: %v", err)
	}
	if d := parseDryRun(t, out); !strings.HasSuffix(d.URL, "/v1/incidents") {
		t.Errorf("url = %q", d.URL)
	}
}

func TestAPIAddsSchemeToAPIHost(t *testing.T) {
	root := newRootCmd("test", "0.0.0-test", miniSpec(t), nil)

	out, _, err := runRoot(t, root, "fhcli", "--api-host", "api.staging.firehydrant.io/", "api", "incidents", "list-incidents", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.URL != "https://api.staging.firehydrant.io/v1/incidents" {
		t.Errorf("url = %q", d.URL)
	}
}

func TestAPIEscapesPathArgs(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "a/b", "r 1", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.URL != ts.URL+"/v1/incidents/a%2Fb/roles/r%201" {
		t.Errorf("url = %q", d.URL)
	}
}

func TestAPIRequiresPathArgs(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "inc-1")

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "role_id is required") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAPIRejectsExtraArgs(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "list-incidents", "surprise")

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, `unexpected argument \"surprise\"`) {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAPIDataMergesWithFlagOverrides(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident",
		"--data", `{"name":"old","labels":{"team":"db"}}`, "--name", "new", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"name": "new", "labels": map[string]any{"team": "db"}}
	if d := parseDryRun(t, out); !reflect.DeepEqual(d.Body, want) {
		t.Errorf("body = %v, want %v", d.Body, want)
	}
}

func TestAPIDataFromFile(t *testing.T) {
	ts := failIfCalled(t)
	path := filepath.Join(t.TempDir(), "incident.json")
	if err := os.WriteFile(path, []byte(`{"name":"from file"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--data", "@"+path, "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.Body["name"] != "from file" {
		t.Errorf("body = %v", d.Body)
	}
}

func TestAPIDataFromStdin(t *testing.T) {
	ts := failIfCalled(t)
	old := stdinReader
	stdinReader = strings.NewReader(`{"name":"piped"}`)
	t.Cleanup(func() { stdinReader = old })

	out, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--data", "-", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.Body["name"] != "piped" {
		t.Errorf("body = %v", d.Body)
	}
}

func TestAPIDataMustBeJSONObject(t *testing.T) {
	ts := failIfCalled(t)
	old := stdinReader
	t.Cleanup(func() { stdinReader = old })

	for _, data := range []string{`[1]`, `null`, `not json`, `-`} {
		stdinReader = strings.NewReader("") // only read for "-": empty stdin

		_, stderr, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--data", data)

		if err == nil {
			t.Errorf("--data %s: expected an error", data)
		}
		if !strings.Contains(stderr, "--data must be a JSON object") {
			t.Errorf("--data %s: stderr = %q", data, stderr)
		}
	}
}

func TestAPIDataRejectedWhenOperationHasNoBody(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "list-incidents", "--data", `{}`)

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "list_incidents takes no request body") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAPISendsExplicitFalseBool(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "close-incident", "inc-1", "--notify=false", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.Body["notify"] != false {
		t.Errorf("explicit --notify=false must be sent, body = %v", d.Body)
	}

	out, _, err = runMini(t, ts.URL, "api", "incidents", "close-incident", "inc-1", "--milestone", "resolved", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := parseDryRun(t, out).Body["notify"]; present {
		t.Error("unset --notify must not be sent")
	}
}

func TestAPISendsRequest(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"inc-new"}`))
	}))
	defer ts.Close()

	out, _, err := runMini(t, ts.URL, "api", "incidents", "create-incident", "--name", "DB down")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "inc-new") {
		t.Errorf("stdout = %q", out)
	}
	if gotMethod != "POST" || gotPath != "/v1/incidents" || gotAuth != "test-token" {
		t.Errorf("request = %s %s auth=%q", gotMethod, gotPath, gotAuth)
	}
	if gotBody["name"] != "DB down" {
		t.Errorf("sent body = %v", gotBody)
	}
}

func TestAPIDeleteRequiresYes(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "delete-incident-role", "inc-1", "role-1")

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "delete_incident_role is destructive; re-run with --yes") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAPIDryRunOfDeleteNeedsNoYes(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "delete-incident-role", "inc-1", "role-1", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := parseDryRun(t, out); d.Method != "DELETE" {
		t.Errorf("method = %q", d.Method)
	}
}

func TestAPIDeleteWithYesSends(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	out, _, err := runMini(t, ts.URL, "api", "incidents", "delete-incident-role", "inc-1", "role-1", "--yes")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "DELETE" || gotPath != "/v1/incidents/inc-1/roles/role-1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if strings.TrimSpace(out) != "{}" {
		t.Errorf("stdout = %q, want {}", out)
	}
}

func TestAPINon2xxExitsNonZero(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"incident not found"}`))
	}))
	defer ts.Close()

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "inc-1", "role-1")

	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "404") || !strings.Contains(stderr, "incident not found") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAPIReportsInvalidSpecOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "fhcli"), 0o755)
	os.WriteFile(filepath.Join(dir, "fhcli", "spec.json"), []byte("garbage"), 0o644)

	_, stderr, err := runCmdFull(t, "http://127.0.0.1:1", "api", "incidents", "list-incidents")

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "fhcli spec update") {
		t.Errorf("stderr should explain how to recover, got %q", stderr)
	}
}

func TestEventWorksWithInvalidSpecOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "fhcli"), 0o755)
	os.WriteFile(filepath.Join(dir, "fhcli", "spec.json"), []byte("garbage"), 0o644)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated) // CreateChangeEvent responds 201
		w.Write([]byte(`{"id":"evt-ok"}`))
	}))
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "event", "deployed")

	if err != nil {
		t.Fatalf("event must not depend on the spec: %v", err)
	}
	if !strings.Contains(out, "evt-ok") {
		t.Errorf("stdout = %q", out)
	}
}

// incidentServer serves GET /v1/incidents/623 -> {"id":"uuid-623"} and records
// every other request path.
func incidentServer(t *testing.T, lookupStatus int) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/incidents/623" {
			w.WriteHeader(lookupStatus)
			if lookupStatus == http.StatusOK {
				w.Write([]byte(`{"id":"uuid-623","number":623}`))
			} else {
				w.Write([]byte(`{"detail":"Record not found"}`))
			}
			return
		}
		w.Write([]byte(`{"id":"role-1"}`))
	}))
	t.Cleanup(ts.Close)
	return ts, &paths
}

func TestAPIResolvesIncidentNumberToID(t *testing.T) {
	ts, paths := incidentServer(t, http.StatusOK)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "623", "role-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"GET /v1/incidents/623", "GET /v1/incidents/uuid-623/roles/role-1"}
	if !reflect.DeepEqual(*paths, want) {
		t.Errorf("requests = %v, want %v", *paths, want)
	}
	if !strings.Contains(out, "role-1") {
		t.Errorf("stdout = %q", out)
	}
}

func TestAPIPassesIncidentIDThrough(t *testing.T) {
	ts, paths := incidentServer(t, http.StatusOK)

	_, _, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "7c89ed2d-91d9", "role-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"GET /v1/incidents/7c89ed2d-91d9/roles/role-1"}; !reflect.DeepEqual(*paths, want) {
		t.Errorf("requests = %v, want %v (no lookup for a non-numeric id)", *paths, want)
	}
}

func TestAPIIncidentNumberLookupFailureExitsNonZero(t *testing.T) {
	ts, paths := incidentServer(t, http.StatusNotFound)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "623", "role-1")

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "resolve incident number 623") || !strings.Contains(stderr, "404") {
		t.Errorf("stderr = %q", stderr)
	}
	if want := []string{"GET /v1/incidents/623"}; !reflect.DeepEqual(*paths, want) {
		t.Errorf("requests = %v, want only the lookup", *paths)
	}
}

func TestAPIIncidentNumberWithDataFromStdin(t *testing.T) {
	ts, paths := incidentServer(t, http.StatusOK)
	old := stdinReader
	stdinReader = strings.NewReader(`{"milestone":"resolved"}`)
	t.Cleanup(func() { stdinReader = old })

	_, _, err := runMini(t, ts.URL, "api", "incidents", "close-incident", "623", "--data", "-")

	if err != nil {
		t.Fatalf("stdin must be read once, before the lookup: %v", err)
	}
	if want := []string{"GET /v1/incidents/623", "PUT /v1/incidents/uuid-623/close"}; !reflect.DeepEqual(*paths, want) {
		t.Errorf("requests = %v, want %v", *paths, want)
	}
}

func TestAPIDryRunNotesIncidentNumber(t *testing.T) {
	ts := failIfCalled(t)

	out, _, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "623", "role-1", "--dry-run")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var d struct {
		URL  string `json:"url"`
		Note string `json:"note"`
	}
	json.Unmarshal([]byte(out), &d)
	if d.URL != ts.URL+"/v1/incidents/623/roles/role-1" {
		t.Errorf("url = %q", d.URL)
	}
	if d.Note != "incident number 623 is resolved to its ID when sent" {
		t.Errorf("note = %q", d.Note)
	}
}

func TestAPIDeleteRefusalHappensBeforeNumberLookup(t *testing.T) {
	ts := failIfCalled(t)

	_, stderr, err := runMini(t, ts.URL, "api", "incidents", "delete-incident-role", "623", "role-1")

	if err == nil || !strings.Contains(stderr, "re-run with --yes") {
		t.Errorf("err=%v stderr=%q, want the --yes refusal with no requests", err, stderr)
	}
}

func TestAPIAcceptsHashIncidentNumber(t *testing.T) {
	ts, paths := incidentServer(t, http.StatusOK)

	_, _, err := runMini(t, ts.URL, "api", "incidents", "get-incident-role", "#623", "role-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"GET /v1/incidents/623", "GET /v1/incidents/uuid-623/roles/role-1"}
	if !reflect.DeepEqual(*paths, want) {
		t.Errorf("requests = %v, want %v", *paths, want)
	}
}

// ticketServer resolves incident 623 and records each POSTed ticket body.
func ticketServer(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/incidents/623":
			w.Write([]byte(`{"id":"uuid-623","number":623}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ticketing/tickets":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			bodies = append(bodies, b)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"ticket-1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(ts.Close)
	return ts, &bodies
}

func TestAPIResolvesRelatedToIncidentNumber(t *testing.T) {
	for _, ref := range []string{"incident/623", "incident/#623"} {
		ts, bodies := ticketServer(t)

		_, stderr, err := runCmdFull(t, ts.URL, "api", "ticketing", "create-ticket", "--summary", "Fix it", "--type", "follow_up", "--related-to", ref)

		if err != nil {
			t.Fatalf("%s: %v %s", ref, err, stderr)
		}
		if len(*bodies) != 1 || (*bodies)[0]["related_to"] != "incident/uuid-623" {
			t.Errorf("%s: bodies = %v, want related_to incident/uuid-623", ref, *bodies)
		}
	}
}

func TestAPIResolvesRelatedToIncidentNumberInData(t *testing.T) {
	ts, bodies := ticketServer(t)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "ticketing", "create-ticket", "--data", `{"summary":"Fix it","type":"follow_up","related_to":"incident/623"}`)

	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	if len(*bodies) != 1 || (*bodies)[0]["related_to"] != "incident/uuid-623" {
		t.Errorf("bodies = %v", *bodies)
	}
}

func TestAPIRelatedToIncidentIDPassesThrough(t *testing.T) {
	ts, bodies := ticketServer(t)

	_, _, err := runCmdFull(t, ts.URL, "api", "ticketing", "create-ticket", "--summary", "Fix it", "--related-to", "incident/7c89ed2d-91d9")

	if err != nil || len(*bodies) != 1 || (*bodies)[0]["related_to"] != "incident/7c89ed2d-91d9" {
		t.Errorf("err = %v, bodies = %v; a UUID reference must be sent unchanged", err, *bodies)
	}
}

func TestAPIDryRunNotesRelatedToIncidentNumber(t *testing.T) {
	stdout, _, err := runCmdFull(t, "http://127.0.0.1:1", "api", "ticketing", "create-ticket", "--summary", "Fix it", "--related-to", "incident/#623", "--dry-run")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "incident number 623 is resolved to its ID when sent") {
		t.Errorf("dry-run = %s, want a note that the number will be resolved", stdout)
	}
}

func TestCuratedIncidentCommandsAcceptHashNumber(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"uuid-623","number":623}`))
	}))
	t.Cleanup(ts.Close)

	_, stderr, err := runCmdFull(t, ts.URL, "incidents", "get", "#623")

	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	if len(paths) == 0 || paths[0] != "/v1/incidents/623" {
		t.Errorf("paths = %v, want /v1/incidents/623 (the # stripped)", paths)
	}
}
