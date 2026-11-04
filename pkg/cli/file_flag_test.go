package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noteServer records the body of each request it receives.
func noteServer(t *testing.T) (*httptest.Server, *[]map[string]any, *[]string) {
	t.Helper()
	var bodies []map[string]any
	var queries []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"note-1","data":[]}`))
	}))
	t.Cleanup(ts.Close)
	return ts, &bodies, &queries
}

const noteText = "## Findings\n\nQueue `SIGNALS_LOW_PRIORITY` backed up — \"CoverageGapNotifier\" fan-out.\n"

func TestStringBodyFlagReadsFile(t *testing.T) {
	ts, bodies, _ := noteServer(t)
	path := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(path, []byte(noteText), 0o600)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "incidents", "create-incident-note", "inc-1", "--body", "@"+path, "--visibility", "private_to_org")

	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	if len(*bodies) != 1 || (*bodies)[0]["body"] != noteText {
		t.Errorf("bodies = %v, want the file's exact contents", *bodies)
	}
}

func TestStringBodyFlagReadsStdin(t *testing.T) {
	ts, bodies, _ := noteServer(t)
	old := stdinReader
	stdinReader = strings.NewReader(noteText)
	t.Cleanup(func() { stdinReader = old })

	_, stderr, err := runCmdFull(t, ts.URL, "api", "incidents", "create-incident-note", "inc-1", "--body", "@-")

	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	if len(*bodies) != 1 || (*bodies)[0]["body"] != noteText {
		t.Errorf("bodies = %v", *bodies)
	}
}

func TestStringBodyFlagMissingFileIsAnError(t *testing.T) {
	ts, bodies, _ := noteServer(t)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "incidents", "create-incident-note", "inc-1", "--body", "@oncall please look")

	if err == nil || !strings.Contains(stderr, "--data") {
		t.Errorf("err = %v, stderr = %s; want an error pointing at --data for literal @ values", err, stderr)
	}
	if len(*bodies) != 0 {
		t.Error("nothing should be sent when the @file can't be read")
	}
}

func TestStdinCanOnlyBeReadOnce(t *testing.T) {
	ts, bodies, _ := noteServer(t)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "incidents", "create-incident-note", "inc-1", "--data", "-", "--body", "@-")

	if err == nil || !strings.Contains(stderr, "stdin") || len(*bodies) != 0 {
		t.Errorf("err = %v, stderr = %s; want a clear error when two flags read stdin", err, stderr)
	}
}

func TestQueryFlagStartingWithAtIsLiteral(t *testing.T) {
	ts, _, queries := noteServer(t)

	_, _, err := runCmdFull(t, ts.URL, "api", "incidents", "list-incidents", "--query", "@here")

	if err != nil || len(*queries) != 1 || !strings.Contains((*queries)[0], "query=%40here") {
		t.Errorf("err = %v, queries = %v; query flags must not read files", err, *queries)
	}
}
