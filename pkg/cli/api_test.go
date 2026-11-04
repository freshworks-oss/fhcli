package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/firehydrant/fhcli/pkg/spec"
	"github.com/urfave/cli/v3"
)

func miniSpec(t *testing.T) *spec.Spec {
	t.Helper()
	data, err := os.ReadFile("../spec/testdata/mini.json")
	if err != nil {
		t.Fatal(err)
	}
	sp, err := spec.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func noopRun(ctx context.Context, cmd *cli.Command, op spec.Operation, bindings []flagBinding) error {
	return nil
}

func findCmd(t *testing.T, cmd *cli.Command, names ...string) *cli.Command {
	t.Helper()
	for _, name := range names {
		var next *cli.Command
		for _, c := range cmd.Commands {
			if c.Name == name {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("command %q not found under %q", name, cmd.Name)
		}
		cmd = next
	}
	return cmd
}

func flagNames(cmd *cli.Command) []string {
	var out []string
	for _, f := range cmd.Flags {
		out = append(out, f.Names()[0])
	}
	return out
}

func flagUsage(t *testing.T, cmd *cli.Command, name string) string {
	t.Helper()
	for _, f := range cmd.Flags {
		if f.Names()[0] == name {
			if u, ok := f.(interface{ GetUsage() string }); ok {
				return u.GetUsage()
			}
		}
	}
	t.Fatalf("flag --%s not found on %q", name, cmd.Name)
	return ""
}

func TestCommandName(t *testing.T) {
	cases := map[string]string{
		"list_incidents":          "list-incidents",
		"Incident Settings":       "incident-settings",
		"Metrics/Reporting":       "metrics-reporting",
		"SCIM":                    "scim",
		"startIndex":              "start-index",
		"custom_fields[field_id]": "custom-fields-field-id",
	}
	for in, want := range cases {
		if got := commandName(in); got != want {
			t.Errorf("commandName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPICmdGroupsOperationsByKebabTag(t *testing.T) {
	api := newAPICmd(miniSpec(t), nil, noopRun)

	var groups []string
	for _, g := range api.Commands {
		groups = append(groups, g.Name)
	}
	if !reflect.DeepEqual(groups, []string{"incident-settings", "incidents", "metrics-reporting"}) {
		t.Errorf("groups = %v", groups)
	}

	var ops []string
	for _, c := range findCmd(t, api, "incidents").Commands {
		ops = append(ops, c.Name)
	}
	want := []string{"close-incident", "create-incident", "delete-incident-role", "get-incident-role", "list-incidents"}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("incidents ops = %v, want %v", ops, want)
	}
}

func TestAPICmdOperationHelp(t *testing.T) {
	cmd := findCmd(t, newAPICmd(miniSpec(t), nil, noopRun), "incidents", "get-incident-role")

	if cmd.ArgsUsage != "<incident_id> <role_id>" {
		t.Errorf("ArgsUsage = %q", cmd.ArgsUsage)
	}
	if cmd.Usage != "Get an incident role" {
		t.Errorf("Usage = %q", cmd.Usage)
	}
	if !strings.Contains(cmd.Description, "GET /v1/incidents/{incident_id}/roles/{role_id}") {
		t.Errorf("Description should show the route, got %q", cmd.Description)
	}
}

func TestAPICmdQueryFlags(t *testing.T) {
	cmd := findCmd(t, newAPICmd(miniSpec(t), nil, noopRun), "incidents", "list-incidents")

	// "token" is a query param in the fixture but would shadow the root --token flag.
	want := []string{"page", "status", "tags", "custom-fields-field-id", "custom-fields-value", "data", "dry-run", "yes"}
	if got := flagNames(cmd); !reflect.DeepEqual(got, want) {
		t.Errorf("flags = %v, want %v", got, want)
	}
	if _, ok := cmd.Flags[0].(*cli.IntFlag); !ok {
		t.Errorf("--page should be an IntFlag, got %T", cmd.Flags[0])
	}
	if _, ok := cmd.Flags[2].(*cli.StringSliceFlag); !ok {
		t.Errorf("--tags should be a StringSliceFlag, got %T", cmd.Flags[2])
	}
	if u := flagUsage(t, cmd, "status"); !strings.Contains(u, "one of: open, resolved") {
		t.Errorf("--status usage should list the enum, got %q", u)
	}
}

func TestAPICmdBodyFlagsSkipNestedAndReserved(t *testing.T) {
	cmd := findCmd(t, newAPICmd(miniSpec(t), nil, noopRun), "incidents", "create-incident")

	// labels (object) and impacts (array of object) are --data only; the body
	// field named "data" is --data only because --data is ours.
	want := []string{"customer-impact", "name", "priority", "tag-list", "data", "dry-run", "yes"}
	if got := flagNames(cmd); !reflect.DeepEqual(got, want) {
		t.Errorf("flags = %v, want %v", got, want)
	}
	if u := flagUsage(t, cmd, "name"); !strings.Contains(u, "(required)") {
		t.Errorf("--name usage should mark it required, got %q", u)
	}
}

func TestAPICmdSkipsBracketedFormFields(t *testing.T) {
	cmd := findCmd(t, newAPICmd(miniSpec(t), nil, noopRun), "incidents", "close-incident")

	want := []string{"milestone", "notify", "data", "dry-run", "yes"}
	if got := flagNames(cmd); !reflect.DeepEqual(got, want) {
		t.Errorf("flags = %v, want %v (fields[label] must be --data only)", got, want)
	}
}

func TestAPICmdPassesOperationToRunner(t *testing.T) {
	var gotOp spec.Operation
	var gotPage int
	var gotBindings []flagBinding
	run := func(ctx context.Context, cmd *cli.Command, op spec.Operation, bindings []flagBinding) error {
		gotOp, gotPage, gotBindings = op, cmd.Int("page"), bindings
		return nil
	}
	root := &cli.Command{Name: "fhcli", Commands: []*cli.Command{newAPICmd(miniSpec(t), nil, run)}}

	_, _, err := runRoot(t, root, "fhcli", "api", "incidents", "list-incidents", "--page", "2")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotOp.ID != "list_incidents" || gotPage != 2 {
		t.Errorf("runner got op=%q page=%d", gotOp.ID, gotPage)
	}
	if len(gotBindings) != 5 || gotBindings[0].flag != "page" || gotBindings[0].inBody {
		t.Errorf("bindings = %+v", gotBindings)
	}
}

func TestAPICmdReportsSpecLoadError(t *testing.T) {
	called := false
	run := func(ctx context.Context, cmd *cli.Command, op spec.Operation, bindings []flagBinding) error {
		called = true
		return nil
	}
	root := &cli.Command{Name: "fhcli", Commands: []*cli.Command{newAPICmd(nil, errors.New("bad spec"), run)}}

	_, stderr, err := runRoot(t, root, "fhcli", "api", "incidents", "list-incidents")

	if err == nil {
		t.Error("expected an error so the CLI exits non-zero")
	}
	if !strings.Contains(stderr, "bad spec") {
		t.Errorf("stderr = %q, want the load error", stderr)
	}
	if called {
		t.Error("runner must not be called when the spec failed to load")
	}
}

func TestAPICmdRegistersEveryEmbeddedOperation(t *testing.T) {
	sp, err := spec.Load()
	if err != nil {
		t.Fatal(err)
	}
	// FHCLI_TEST_SPEC deliberately installs another spec as the override.
	if sp.Source != spec.EmbeddedSource && os.Getenv("FHCLI_TEST_SPEC") == "" {
		t.Fatalf("expected the embedded spec (TestMain isolates XDG_CONFIG_HOME), got %q", sp.Source)
	}

	api := newAPICmd(sp, nil, noopRun)
	reserved := reservedFlagNames()
	ours := map[string]bool{"data": true, "dry-run": true, "yes": true}
	leaves := 0
	seen := map[string]bool{}

	for _, g := range api.Commands {
		for _, op := range g.Commands {
			leaves++
			key := g.Name + " " + op.Name
			if seen[key] {
				t.Errorf("duplicate command %q", key)
			}
			seen[key] = true

			flags := map[string]bool{}
			for _, f := range op.Flags {
				for _, n := range f.Names() {
					if flags[n] {
						t.Errorf("%s: duplicate flag --%s", key, n)
					}
					flags[n] = true
					if reserved[n] && !ours[n] {
						t.Errorf("%s: --%s shadows a root flag", key, n)
					}
				}
			}
		}
	}

	if leaves != len(sp.Operations) {
		t.Errorf("registered %d commands for %d operations", leaves, len(sp.Operations))
	}
}

func TestAPICmdHelpListsDataOnlyBodyFields(t *testing.T) {
	api := newAPICmd(miniSpec(t), nil, noopRun)

	create := findCmd(t, api, "incidents", "create-incident").Description
	for _, want := range []string{"Set these body fields with --data:", "data (string)", "impacts (array of object)", "labels (object)"} {
		if !strings.Contains(create, want) {
			t.Errorf("create-incident help should mention %q, got:\n%s", want, create)
		}
	}

	// Bracketed form fields fold to their root key.
	if closeHelp := findCmd(t, api, "incidents", "close-incident").Description; !strings.Contains(closeHelp, "fields (object)") {
		t.Errorf("close-incident help should mention fields (object), got:\n%s", closeHelp)
	}

	if list := findCmd(t, api, "incidents", "list-incidents").Description; strings.Contains(list, "--data") {
		t.Errorf("an operation without --data-only fields should not list any, got:\n%s", list)
	}
}

func TestAPIFlagHelpIgnoresBackticksInDescriptions(t *testing.T) {
	root := &cli.Command{Name: "fhcli", Commands: []*cli.Command{newAPICmd(miniSpec(t), nil, noopRun)}}

	out, _, err := runRoot(t, root, "fhcli", "api", "incidents", "list-incidents", "--help")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// urfave/cli treats `text` in a usage string as the value placeholder, which
	// would render as "--page per_page".
	if !strings.Contains(out, "--page int") || strings.Contains(out, "--page per_page") {
		t.Errorf("--page help is wrong:\n%s", out)
	}
	if !strings.Contains(out, "'per_page'") {
		t.Errorf("backticks should render as quotes:\n%s", out)
	}
}

func TestAPICmdHelpShowsResponseFields(t *testing.T) {
	api := newAPICmd(miniSpec(t), nil, noopRun)

	if d := findCmd(t, api, "incidents", "list-incidents").Description; !strings.Contains(d, "Response: data[] (id, name, number), pagination") {
		t.Errorf("list-incidents help should show paginated response fields, got:\n%s", d)
	}
	if d := findCmd(t, api, "incidents", "get-incident-role").Description; !strings.Contains(d, "Response: id, role, user") {
		t.Errorf("get-incident-role help should show response fields, got:\n%s", d)
	}
	if d := findCmd(t, api, "incidents", "close-incident").Description; strings.Contains(d, "Response:") {
		t.Errorf("an operation without a documented response shows no Response line, got:\n%s", d)
	}
}

func TestDescribeResponseTruncatesLongFieldLists(t *testing.T) {
	var items []string
	for i := 0; i < 14; i++ {
		items = append(items, fmt.Sprintf("f%02d", i))
	}
	op := spec.Operation{ResponseFields: []string{"data", "pagination"}, ResponseItemFields: items}

	got := describeResponse(op)

	want := "Response: data[] (f00, f01, f02, f03, f04, f05, f06, f07, f08, f09, f10, f11, … 14 fields), pagination"
	if got != want {
		t.Errorf("describeResponse = %q\nwant %q", got, want)
	}
}
