package spec

// overlay corrects and annotates the published API spec with facts verified
// against the laddertruck source, so `fhcli api` help and flags are right
// before the spec itself is fixed (FH-6311). Each entry should be removed
// once the spec carries the fix; TestOverlayEntriesAreNeeded fails when an
// entry becomes redundant.
var overlay = []overlayEntry{
	{
		Operation: "create_ticket",
		Fields: map[string]fieldFix{
			// Ticketing::Ticket::TYPES (app/models/ticketing/ticket.rb).
			"type": {Enum: []string{"incident", "task", "follow_up"}},
		},
		SeeAlso: []string{"list_ticketing_projects"},
		Notes: []string{
			"Follow-ups (action items) are tickets: --type follow_up --related-to incident/<incident id or number>. " +
				"--project-id comes from list_ticketing_projects.",
		},
	},
	{
		Operation: "list_tickets",
		SeeAlso:   []string{"create_ticket"},
		Notes:     []string{"Follow-ups are tickets with type follow_up; there is no filter by incident, so filter related_to with jq."},
	},
	{
		Operation: "update_incident_retrospective_field",
		Fields: map[string]fieldFix{
			// Grape declares `types: [Numeric, String, [String], [Hash]]`
			// (app/api/public_api/v1/incidents/retrospectives.rb); the spec
			// documents only the first, integer.
			"value": {Type: "string"},
		},
		SeeAlso: []string{"create_incident_retrospective_dynamic_input", "list_incident_retrospectives"},
		Notes: []string{
			"--value takes text. Multi-select values (arrays) must go through --data '{\"value\": [...]}'.",
			"Dynamic input groups (e.g. Contributing Factors) take two steps: create_incident_retrospective_dynamic_input " +
				"adds an empty entry, then set it here with --data '{\"dynamic_input_field_id\": \"<entry id>\", \"value\": \"...\"}'.",
		},
	},
	{
		Operation: "create_incident_retrospective_dynamic_input",
		SeeAlso:   []string{"update_incident_retrospective_field"},
		Notes:     []string{"Takes no body: it adds an empty entry to the group. Fill it with update_incident_retrospective_field and dynamic_input_field_id."},
	},
	{
		Operation: "bulk_update_incident_milestones",
		SeeAlso:   []string{"list_lifecycle_phases"},
		Notes:     []string{"Valid milestone slugs and their order come from list_lifecycle_phases."},
	},
	{
		Operation: "list_lifecycle_phases",
		SeeAlso:   []string{"bulk_update_incident_milestones"},
		Notes:     []string{"Lists the milestone types (slugs) in lifecycle order, as used by bulk_update_incident_milestones."},
	},
	{
		// The spec says "Creates a comment for a project", a stale locale
		// string (packs/conversations/app/locales/conversations.en.yml).
		// Conversations hang off alerts, incidents, timeline events and
		// retrospective questions (their entities expose conversations[]).
		Operation: "create_comment",
		SeeAlso:   []string{"get_alert", "list_comments"},
		Notes: []string{
			"Adds a comment to a conversation: an alert's, an incident's, or a timeline event's. " +
				"conversation_id is the resource's .conversations[].id (e.g. from get_alert), not the alert or incident ID.",
		},
	},
	{
		Operation: "get_alert",
		SeeAlso:   []string{"create_comment"},
		Notes:     []string{"To comment on this alert, pass .conversations[0].id to create_comment."},
	},
	{
		// The spec marks only name required, but the endpoint needs a time
		// zone unless rotations are given; a request without one passes
		// --dry-run and fails with a 500.
		Operation: "create_team_on_call_schedule",
		SeeAlso:   []string{"create_team_escalation_policy", "list_team_on_call_schedules"},
		Notes: []string{
			"Required unless rotations is given: --time-zone (an IANA name, e.g. America/Los_Angeles). Omitting it returns a 500, not a 422.",
			"A weekly strategy ignores --start-time: the first shift is the most recent handoff, and the first member listed is first on call.",
			"The schedule id from this call is what create_team_escalation_policy targets (type OnCallSchedule).",
		},
	},
	{
		Operation: "create_incident_note",
		Notes: []string{
			"Notes default to private_to_org. open_to_public is visible outside your organization; " +
				"pass --visibility explicitly when posting during an incident.",
		},
	},
}

type overlayEntry struct {
	Operation string
	Fields    map[string]fieldFix
	SeeAlso   []string
	Notes     []string
}

// fieldFix replaces a parameter's documented type and/or enum.
type fieldFix struct {
	Type string
	Enum []string
}

// applyOverlay patches s in place. Entries for operations or fields the spec
// doesn't have are skipped, so a newer spec never breaks the CLI.
func applyOverlay(s *Spec, entries []overlayEntry) {
	index := make(map[string]int, len(s.Operations))
	for i, op := range s.Operations {
		index[op.ID] = i
	}
	for _, e := range entries {
		i, ok := index[e.Operation]
		if !ok {
			continue
		}
		op := &s.Operations[i]
		op.SeeAlso = append(op.SeeAlso, e.SeeAlso...)
		op.Notes = append(op.Notes, e.Notes...)
		for _, group := range [][]Param{op.PathParams, op.QueryParams, op.BodyFields} {
			for j := range group {
				fix, ok := e.Fields[group[j].Name]
				if !ok {
					continue
				}
				if fix.Type != "" {
					group[j].Type = fix.Type
					group[j].ItemType = ""
				}
				if len(fix.Enum) > 0 && len(group[j].Enum) == 0 {
					group[j].Enum = fix.Enum
				}
			}
		}
	}
}
