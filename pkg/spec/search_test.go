package spec

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func resultIDs(rs []Result) []string {
	ids := make([]string, len(rs))
	for i, r := range rs {
		ids[i] = r.Operation.ID
	}
	return ids
}

func TestTokenize(t *testing.T) {
	got := tokenize("List the Severity_Matrix impacts for /v1/nunc-connections/{incident_id}")
	want := []string{"list", "severity", "matrix", "impact", "v1", "nunc", "connection", "incident", "id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokenize = %v, want %v", got, want)
	}
}

func TestSingularize(t *testing.T) {
	for in, want := range map[string]string{
		"pages": "page", "policies": "policy", "impacts": "impact",
		"status": "status", "process": "process", "matrix": "matrix", "is": "is",
	} {
		if got := singularize(in); got != want {
			t.Errorf("singularize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchFindsBySummaryWords(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("close incident", 10))

	if len(got) == 0 || got[0] != "close_incident" {
		t.Errorf("results = %v, want close_incident first", got)
	}
}

func TestSearchPrefersOperationsMatchingMoreQueryWords(t *testing.T) {
	s := loadFixture(t)

	// "role" appears only in the role operations; "incident" is everywhere.
	got := resultIDs(s.Search("incident role", 10))

	if len(got) < 2 || !(got[0] == "get_incident_role" || got[0] == "delete_incident_role") ||
		!(got[1] == "get_incident_role" || got[1] == "delete_incident_role") {
		t.Errorf("results = %v, want both incident role operations first", got)
	}
}

func TestSearchMatchesParameterNames(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("milestone", 10))

	if !reflect.DeepEqual(got, []string{"close_incident"}) {
		t.Errorf("results = %v, want only close_incident (has a milestone body field)", got)
	}
}

func TestSearchMatchesTag(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("metrics", 10))

	if !reflect.DeepEqual(got, []string{"get_mttx"}) {
		t.Errorf("results = %v, want get_mttx via its Metrics/Reporting tag", got)
	}
}

func TestSearchIgnoresStopWordsAndCase(t *testing.T) {
	s := loadFixture(t)

	got := resultIDs(s.Search("Close AN Incident", 1))

	if !reflect.DeepEqual(got, []string{"close_incident"}) {
		t.Errorf("results = %v", got)
	}
}

func TestSearchLimit(t *testing.T) {
	s := loadFixture(t)

	if got := s.Search("incident", 2); len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestSearchNoMatches(t *testing.T) {
	s := loadFixture(t)

	if got := s.Search("kubernetes", 10); len(got) != 0 {
		t.Errorf("results = %v, want none", resultIDs(got))
	}
	if got := s.Search("the a of", 10); len(got) != 0 {
		t.Errorf("a query of only stop words should match nothing, got %v", resultIDs(got))
	}
}

func TestSearchIsDeterministic(t *testing.T) {
	s := loadFixture(t)

	first := resultIDs(s.Search("incident", 10))
	for i := 0; i < 20; i++ {
		if got := resultIDs(s.Search("incident", 10)); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d = %v, first = %v", i, got, first)
		}
	}
}

func TestSearchScoresAreDescending(t *testing.T) {
	s := loadFixture(t)

	rs := s.Search("incident", 10)
	for i := 1; i < len(rs); i++ {
		if rs[i].Score > rs[i-1].Score {
			t.Errorf("score[%d]=%v > score[%d]=%v", i, rs[i].Score, i-1, rs[i-1].Score)
		}
	}
}

// embeddedOrTestSpec returns the raw embedded spec, or the spec file named by
// FHCLI_TEST_SPEC so the whole-spec tests can run against a freshly fetched
// spec (e.g. `curl -o /tmp/live.json https://api.firehydrant.io/v1/swagger_doc`).
func embeddedOrTestSpec(t *testing.T) []byte {
	t.Helper()
	if path := os.Getenv("FHCLI_TEST_SPEC"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return embedded
}

func loadEmbeddedSpec(t *testing.T) *Spec {
	t.Helper()
	s, err := Parse(embeddedOrTestSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func containsAll(got []string, want ...string) []string {
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	var missing []string
	for _, id := range want {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func TestSearchRealSpecSeverityMatrix(t *testing.T) {
	s := loadEmbeddedSpec(t)

	got := resultIDs(s.Search("severity matrix", 10))

	if missing := containsAll(got, "get_severity_matrix", "update_severity_matrix", "create_severity_matrix_impact", "list_severity_matrix_conditions"); len(missing) > 0 {
		t.Errorf("top 10 = %v, missing %v", got, missing)
	}
	if got[0] != "get_severity_matrix" && got[0] != "update_severity_matrix" {
		t.Errorf("first = %s, want the matrix itself (get/update) ahead of its sub-resources", got[0])
	}
}

func TestSearchRealSpecStatusPageComponents(t *testing.T) {
	s := loadEmbeddedSpec(t)

	got := resultIDs(s.Search("status page component", 5))

	if missing := containsAll(got, "create_nunc_component_group", "update_nunc_component_group", "delete_nunc_component_group"); len(missing) > 0 {
		t.Errorf("top 5 = %v, missing %v", got, missing)
	}
}

func TestSearchRealSpecIsFast(t *testing.T) {
	s := loadEmbeddedSpec(t)
	start := time.Now()

	for i := 0; i < 10; i++ {
		s.Search("status page component", 10)
	}

	if per := time.Since(start) / 10; per > 50*time.Millisecond {
		t.Errorf("search took %v per query, want < 50ms", per)
	}
}

// A "[DEPRECATED]" marker adds a word to the summary that no query matches,
// which lowers the summary-focus tie-break below the replacement's.
func TestSearchRanksDeprecatedOperationsLast(t *testing.T) {
	s := &Spec{Operations: []Operation{
		{ID: "get_on_call_shift_v1", Tag: "Teams", Summary: "[DEPRECATED] Get an on-call shift"},
		{ID: "get_on_call_shift_v2", Tag: "Teams", Summary: "Get an on-call shift"},
	}}

	got := resultIDs(s.Search("on call shift", 10))

	if !reflect.DeepEqual(got, []string{"get_on_call_shift_v2", "get_on_call_shift_v1"}) {
		t.Errorf("results = %v, want the deprecated operation after its replacement", got)
	}
}

// rank is the 1-based position of id in rs, or 0 if absent.
func rank(rs []Result, id string) int {
	for i, r := range rs {
		if r.Operation.ID == id {
			return i + 1
		}
	}
	return 0
}

// Every operation in the real spec is the top result when you search for
// its own name (operationId with spaces), so an agent that knows or guesses
// the operation's name always lands on it.
func TestSearchRealSpecFindsEveryOperationByName(t *testing.T) {
	s := loadEmbeddedSpec(t)

	for _, op := range s.Operations {
		query := strings.ReplaceAll(op.ID, "_", " ")
		if r := rank(s.Search(query, 0), op.ID); r != 1 {
			t.Errorf("search %q: %s ranked %d, want 1", query, op.ID, r)
		}
	}
}

// Every operation is found by its own summary. Summaries are not unique in
// the spec (e.g. "Create a status page" is both create_nunc_connection and
// create_incident_status_page), so an operation must rank within the group
// that shares its summary.
func TestSearchRealSpecFindsEveryOperationBySummary(t *testing.T) {
	s := loadEmbeddedSpec(t)
	shared := map[string]int{}
	for _, op := range s.Operations {
		shared[strings.Join(tokenize(op.Summary), " ")]++
	}

	for _, op := range s.Operations {
		if strings.TrimSpace(op.Summary) == "" {
			t.Errorf("%s has no summary", op.ID)
			continue
		}
		limit := shared[strings.Join(tokenize(op.Summary), " ")]
		if r := rank(s.Search(op.Summary, 0), op.ID); r == 0 || r > limit {
			t.Errorf("search %q: %s ranked %d, want within the %d operation(s) sharing that summary", op.Summary, op.ID, r, limit)
		}
	}
}

// No word from the spec, and no odd input, makes Search panic, and every
// word that appears in an operation's name finds that operation.
func TestSearchRealSpecHandlesEveryWordAndOddInput(t *testing.T) {
	s := loadEmbeddedSpec(t)
	words := map[string]bool{}
	for _, op := range s.Operations {
		for _, w := range tokenize(op.ID + " " + op.Summary + " " + op.Tag + " " + op.Path) {
			words[w] = true
		}
	}

	for w := range words {
		if len(s.Search(w, 1)) == 0 {
			t.Errorf("search %q found nothing, but the word comes from the spec", w)
		}
	}
	for _, q := range []string{"", "   ", "!!!", "{incident_id}", "/v1/incidents", "インシデント", "émoji 🔥 incident",
		strings.Repeat("incident ", 2000), "SEVERITY_MATRIX", "severity-matrix"} {
		s.Search(q, 10)
	}
	if got := rank(s.Search("severity-matrix", 1), "get_severity_matrix") + rank(s.Search("severity-matrix", 1), "update_severity_matrix"); got == 0 {
		t.Error("kebab-case query should match like spaced words")
	}
}

// Queries from the incident #661 session that missed the right operation.
func TestSearchRealSpecFindsFollowUps(t *testing.T) {
	s := loadEmbeddedSpec(t)

	for _, q := range []string{"create follow up for incident", "incident follow ups", "add action item", "create followup"} {
		if r := rank(s.Search(q, 0), "create_ticket"); r == 0 || r > 3 {
			t.Errorf("search %q: create_ticket ranked %d, want top 3", q, r)
		}
	}
}

func TestSearchRealSpecUnderstandsSynonyms(t *testing.T) {
	s := loadEmbeddedSpec(t)

	for q, id := range map[string]string{
		"postmortem fields":       "update_incident_retrospective_field",
		"list retros":             "list_incident_retrospectives",
		"milestone types":         "list_lifecycle_phases",
		"list ticketing projects": "list_ticketing_projects",
	} {
		if r := rank(s.Search(q, 0), id); r == 0 || r > 5 {
			t.Errorf("search %q: %s ranked %d, want top 5", q, id, r)
		}
	}
}

// Parameter descriptions and enum values are searched too, at low weight.
func TestSearchMatchesParameterDescriptionsAndEnums(t *testing.T) {
	s := &Spec{Operations: []Operation{
		{ID: "make_widget", Summary: "Make a widget", BodyFields: []Param{{Name: "kind", Enum: []string{"sprocket"}, Description: "The flavour of widget"}}},
		{ID: "other", Summary: "Something else"},
	}}

	if got := resultIDs(s.Search("sprocket", 10)); !reflect.DeepEqual(got, []string{"make_widget"}) {
		t.Errorf("enum value search = %v", got)
	}
	if got := resultIDs(s.Search("flavour", 10)); !reflect.DeepEqual(got, []string{"make_widget"}) {
		t.Errorf("param description search = %v", got)
	}
}

// Queries from the 2026-11-03 Signals session: commenting on an alert goes
// through conversations, which nothing in the spec ties to alerts.
func TestSearchRealSpecFindsAlertComments(t *testing.T) {
	s := loadEmbeddedSpec(t)

	for _, q := range []string{"alert note comment", "comment on alert", "add comment to alert"} {
		if r := rank(s.Search(q, 0), "create_comment"); r == 0 || r > 2 {
			t.Errorf("search %q: create_comment ranked %d, want top 2", q, r)
		}
	}
	// Incident notes are their own operation; comments must not displace it.
	if r := rank(s.Search("add note to incident", 0), "create_incident_note"); r != 1 {
		t.Errorf("search %q: create_incident_note ranked %d, want 1", "add note to incident", r)
	}
}
