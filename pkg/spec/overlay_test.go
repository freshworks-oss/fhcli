package spec

import (
	"reflect"
	"strings"
	"testing"
)

func findField(t *testing.T, op Operation, name string) Param {
	t.Helper()
	for _, group := range [][]Param{op.PathParams, op.QueryParams, op.BodyFields} {
		for _, p := range group {
			if p.Name == name {
				return p
			}
		}
	}
	t.Fatalf("%s has no field %q", op.ID, name)
	return Param{}
}

func TestParseRecordsDefaults(t *testing.T) {
	s := loadEmbeddedSpec(t)

	vis := findField(t, findOp(t, s, "create_incident_note"), "visibility")

	if vis.Default != "private_to_org" {
		t.Errorf("visibility default = %q, want private_to_org", vis.Default)
	}
}

func TestOverlayAddsTicketTypeEnum(t *testing.T) {
	s := loadEmbeddedSpec(t)

	typ := findField(t, findOp(t, s, "create_ticket"), "type")

	if !reflect.DeepEqual(typ.Enum, []string{"incident", "task", "follow_up"}) {
		t.Errorf("create_ticket type enum = %v", typ.Enum)
	}
}

func TestOverlayRetypesRetrospectiveFieldValue(t *testing.T) {
	s := loadEmbeddedSpec(t)

	value := findField(t, findOp(t, s, "update_incident_retrospective_field"), "value")

	if value.Type != "string" {
		t.Errorf("value type = %q, want string (the spec says integer; the API takes text)", value.Type)
	}
}

func TestOverlayAddsSeeAlsoAndNotes(t *testing.T) {
	s := loadEmbeddedSpec(t)

	for id, want := range map[string]string{
		"bulk_update_incident_milestones":     "list_lifecycle_phases",
		"create_ticket":                       "list_ticketing_projects",
		"update_incident_retrospective_field": "create_incident_retrospective_dynamic_input",
		"get_alert":                           "create_comment",
		"create_comment":                      "get_alert",
	} {
		op := findOp(t, s, id)
		found := false
		for _, ref := range op.SeeAlso {
			found = found || ref == want
		}
		if !found {
			t.Errorf("%s see-also = %v, want %s", id, op.SeeAlso, want)
		}
	}
	if notes := strings.Join(findOp(t, s, "update_incident_retrospective_field").Notes, " "); !strings.Contains(notes, "dynamic_input_field_id") {
		t.Errorf("retro field notes should explain the dynamic input two-step: %q", notes)
	}
}

// Every overlay entry must still target a real operation and field, and must
// still be needed: when the API spec is fixed upstream, this fails so the
// entry gets removed instead of silently overriding the fix.
func TestOverlayEntriesAreNeeded(t *testing.T) {
	s, err := parse(embeddedOrTestSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]Operation{}
	for _, op := range s.Operations {
		ops[op.ID] = op
	}

	for _, e := range overlay {
		op, ok := ops[e.Operation]
		if !ok {
			t.Errorf("overlay targets unknown operation %s", e.Operation)
			continue
		}
		for _, ref := range e.SeeAlso {
			if _, ok := ops[ref]; !ok {
				t.Errorf("overlay %s: see-also %s is not an operation", e.Operation, ref)
			}
		}
		for name, fix := range e.Fields {
			p := findField(t, op, name)
			if fix.Type != "" && p.Type == fix.Type {
				t.Errorf("overlay %s.%s: spec already types it %s; remove the override", e.Operation, name, fix.Type)
			}
			if len(fix.Enum) > 0 && len(p.Enum) > 0 {
				t.Errorf("overlay %s.%s: spec now has enum %v; remove the override", e.Operation, name, p.Enum)
			}
		}
	}
}

func TestOverlaySkipsMissingOperations(t *testing.T) {
	s := &Spec{Operations: []Operation{{ID: "only_this"}}}

	applyOverlay(s, []overlayEntry{{Operation: "gone", Notes: []string{"x"}}})

	if len(s.Operations[0].Notes) != 0 {
		t.Error("an overlay entry for a missing operation must be ignored, not applied elsewhere")
	}
}
