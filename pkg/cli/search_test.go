package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/firehydrant/fhcli/pkg/spec"
)

type searchOutput struct {
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	Method   string   `json:"method"`
	Path     string   `json:"path"`
	Summary  string   `json:"summary"`
	Required []string `json:"required"`
	Score    float64  `json:"score"`
}

func runSearch(t *testing.T, args ...string) ([]searchOutput, string, error) {
	t.Helper()
	stdout, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), append([]string{"fhcli", "search"}, args...)...)
	var out []searchOutput
	if err == nil {
		if jerr := json.Unmarshal([]byte(stdout), &out); jerr != nil {
			t.Fatalf("stdout is not a JSON array: %v\n%s", jerr, stdout)
		}
	}
	return out, stderr, err
}

func TestSearchCmdReturnsRunnableCommands(t *testing.T) {
	out, stderr, err := runSearch(t, "severity", "matrix")

	if err != nil {
		t.Fatalf("err = %v, stderr = %s", err, stderr)
	}
	if len(out) == 0 {
		t.Fatal("no results")
	}
	first := out[0]
	if first.Command != "fhcli api incident-settings get-severity-matrix" && first.Command != "fhcli api incident-settings update-severity-matrix" {
		t.Errorf("first command = %q", first.Command)
	}
	if first.Path != "/v1/severity_matrix" || first.Summary == "" || first.Method == "" || first.Score <= 0 {
		t.Errorf("first result = %+v", first)
	}
	if first.Args == nil {
		t.Error("args should be [] for an operation with no path params, not null")
	}
}

func TestSearchCmdListsPathParamsAsArgs(t *testing.T) {
	out, _, err := runSearch(t, "add", "note", "to", "incident")

	if err != nil || len(out) == 0 {
		t.Fatalf("err = %v, out = %v", err, out)
	}
	if out[0].Command != "fhcli api incidents create-incident-note" || strings.Join(out[0].Args, ",") != "incident_id" {
		t.Errorf("first = %+v, want create-incident-note with args [incident_id]", out[0])
	}
}

// Each hit names the inputs the call can't be sent without, so an agent can
// build the command from the search result instead of running --help first.
func TestSearchCmdListsRequiredInputs(t *testing.T) {
	out, _, err := runSearch(t, "--limit", "0", "comment", "on", "alert")

	if err != nil {
		t.Fatal(err)
	}
	for _, h := range out {
		if h.Command == "fhcli api conversations create-comment" {
			if strings.Join(h.Required, ",") != "--body" {
				t.Errorf("create-comment required = %v, want [--body]", h.Required)
			}
			return
		}
	}
	t.Fatal("create-comment not found")
}

// Required body fields that have no flag are named as data.<field>, since
// they can only be set with --data.
func TestSearchHitsNameRequiredDataOnlyFields(t *testing.T) {
	op := spec.Operation{ID: "make_widget", Tag: "Widgets", Method: "POST", Path: "/v1/widgets", HasBody: true,
		QueryParams: []spec.Param{{Name: "dry", Type: "boolean"}},
		BodyFields: []spec.Param{
			{Name: "name", Type: "string", Required: true},
			{Name: "parts", Type: "array", ItemType: "object", Required: true},
			{Name: "color", Type: "string"},
		}}

	hit := searchHits([]spec.Result{{Operation: op}})[0]

	if got := strings.Join(hit.Required, ","); got != "--name,data.parts" {
		t.Errorf("required = %q, want --name,data.parts", got)
	}
}

func TestSearchCmdLimit(t *testing.T) {
	out, _, err := runSearch(t, "--limit", "3", "incident")

	if err != nil || len(out) != 3 {
		t.Errorf("len = %d, err = %v; want 3", len(out), err)
	}
}

func TestSearchCmdNoMatchesPrintsEmptyArray(t *testing.T) {
	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "search", "kubernetesxyz")

	if err != nil || strings.TrimSpace(stdout) != "[]" {
		t.Errorf("stdout = %q, err = %v; want []", stdout, err)
	}
}

func TestSearchCmdRequiresAQuery(t *testing.T) {
	_, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "search")

	if err == nil || !strings.Contains(stderr, "fhcli search severity matrix") {
		t.Errorf("err = %v, stderr = %s; want a usage error with an example", err, stderr)
	}
}

func TestSearchCmdNeedsNoCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, err := runSearch(t, "incident")

	if err != nil || strings.Contains(stderr, "API token") {
		t.Errorf("err = %v, stderr = %s; search must work without a token or login", err, stderr)
	}
}

func TestSearchCmdInteractiveTable(t *testing.T) {
	stdout, _, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), "fhcli", "-i", "search", "severity", "matrix")

	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "[") || !strings.Contains(stdout, "fhcli api incident-settings get-severity-matrix") || !strings.Contains(stdout, "Get severity matrix") {
		t.Errorf("stdout = %q, want a command/summary table", stdout)
	}
}

func TestSearchCmdReportsSpecLoadError(t *testing.T) {
	root := newRootCmd("test", "0.0.0-test", nil, errors.New("override spec is invalid; run `fhcli spec update` or delete the file"))

	_, stderr, err := runRoot(t, root, "fhcli", "search", "incident")

	if err == nil || !strings.Contains(stderr, "fhcli spec update") {
		t.Errorf("err = %v, stderr = %s", err, stderr)
	}
}

// Every command `fhcli search` can print actually runs: for each operation in
// the embedded spec, the search hit's command and positional args go through
// the real root command with --dry-run and must produce that operation's
// request. This also exercises the whole generated `fhcli api` surface.
func TestEverySearchHitIsARunnableCommand(t *testing.T) {
	sp, err := spec.Load()
	if err != nil {
		t.Fatal(err)
	}
	const host = "https://api.example.test"

	for _, op := range sp.Operations {
		hit := searchHits([]spec.Result{{Operation: op}})[0]
		words := strings.Fields(hit.Command)
		// Look the command up first: running an unknown command makes urfave
		// exit the whole test binary instead of returning an error.
		if len(words) != 4 || words[0] != "fhcli" || words[1] != "api" || NewRootCmd("test", "0.0.0-test").Command("api").Command(words[2]).Command(words[3]) == nil {
			t.Errorf("%s (%s): search printed a command that isn't registered", hit.Command, op.ID)
			continue
		}
		argv := append([]string{"fhcli", "--token", "test-token", "--api-host", host}, words[1:]...)
		wantPath := op.Path
		for i, a := range hit.Args {
			value := fmt.Sprintf("arg%d", i)
			argv = append(argv, value)
			wantPath = strings.ReplaceAll(wantPath, "{"+a+"}", value)
		}
		argv = append(argv, "--dry-run")

		stdout, stderr, err := runRoot(t, NewRootCmd("test", "0.0.0-test"), argv...)

		if err != nil {
			t.Errorf("%s: %v\nstderr: %s", hit.Command, err, stderr)
			continue
		}
		var got dryRunOutput
		if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil {
			t.Errorf("%s: dry-run output is not JSON: %v\n%s", hit.Command, jerr, stdout)
			continue
		}
		if got.Method != op.Method || got.URL != host+wantPath {
			t.Errorf("%s: dry-run = %s %s, want %s %s", hit.Command, got.Method, got.URL, op.Method, host+wantPath)
		}
		if strings.Contains(got.URL, "{") {
			t.Errorf("%s: unsubstituted path param in %s", hit.Command, got.URL)
		}
	}
}
