package cli

import (
	"strings"
	"testing"

	urfcli "github.com/urfave/cli/v3"
)

func apiCommand(t *testing.T, area, op string) *urfcli.Command {
	t.Helper()
	c := NewRootCmd("test", "0.0.0-test").Command("api").Command(area).Command(op)
	if c == nil {
		t.Fatalf("fhcli api %s %s not found", area, op)
	}
	return c
}

func TestHelpShowsServerDefaults(t *testing.T) {
	c := apiCommand(t, "incidents", "create-incident-note")

	if u := flagUsage(t, c, "visibility"); !strings.Contains(u, "server default: private_to_org") {
		t.Errorf("--visibility usage = %q, want the server default", u)
	}
}

func TestHelpShowsOverlayEnum(t *testing.T) {
	c := apiCommand(t, "ticketing", "create-ticket")

	if u := flagUsage(t, c, "type"); !strings.Contains(u, "one of: incident, task, follow_up") {
		t.Errorf("--type usage = %q, want the ticket types", u)
	}
}

func TestHelpShowsNotesAndSeeAlsoCommands(t *testing.T) {
	c := apiCommand(t, "incidents", "bulk-update-incident-milestones")

	if !strings.Contains(c.Description, "Valid milestone slugs") {
		t.Errorf("description lacks the overlay note:\n%s", c.Description)
	}
	if !strings.Contains(c.Description, "See also: fhcli api incident-settings list-lifecycle-phases") {
		t.Errorf("description lacks the see-also command:\n%s", c.Description)
	}
}

func TestRetrospectiveValueIsATextFlag(t *testing.T) {
	c := apiCommand(t, "retrospectives", "update-incident-retrospective-field")

	for _, f := range c.Flags {
		if f.Names()[0] == "value" {
			if _, ok := f.(*urfcli.StringFlag); !ok {
				t.Errorf("--value is %T, want a string flag", f)
			}
			return
		}
	}
	t.Error("no --value flag")
}

// String body flags read @file and @- (stdin); help must say so, or agents
// fall back to --body "$(cat file)" and its quoting problems.
func TestHelpAdvertisesFileInputOnStringBodyFlags(t *testing.T) {
	c := apiCommand(t, "conversations", "create-comment")

	if u := flagUsage(t, c, "body"); !strings.Contains(u, "@file") || !strings.Contains(u, "@-") {
		t.Errorf("--body usage = %q, want @file and @- advertised", u)
	}
}

// Query flags take @ literally (e.g. a search for "@alice"), so their help
// must not advertise file input.
func TestHelpDoesNotAdvertiseFileInputOnQueryFlags(t *testing.T) {
	c := apiCommand(t, "alerts", "list-alerts")

	if u := flagUsage(t, c, "query"); strings.Contains(u, "@file") {
		t.Errorf("--query usage = %q, should not advertise @file", u)
	}
}
