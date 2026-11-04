package spec

import (
	"os"
	"reflect"
	"testing"
)

func loadFixture(t *testing.T) *Spec {
	t.Helper()
	data, err := os.ReadFile("testdata/mini.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func findOp(t *testing.T, s *Spec, id string) Operation {
	t.Helper()
	for _, op := range s.Operations {
		if op.ID == id {
			return op
		}
	}
	t.Fatalf("operation %q not found", id)
	return Operation{}
}

func byName(ps []Param) map[string]Param {
	m := make(map[string]Param, len(ps))
	for _, p := range ps {
		m[p.Name] = p
	}
	return m
}

func names(ps []Param) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func TestParseOrdersOperationsByTagThenID(t *testing.T) {
	s := loadFixture(t)

	var got []string
	for _, op := range s.Operations {
		got = append(got, op.Tag+"/"+op.ID)
	}

	want := []string{
		"Incident Settings/list_severities",
		"Incidents/close_incident",
		"Incidents/create_incident",
		"Incidents/delete_incident_role",
		"Incidents/get_incident_role",
		"Incidents/list_incidents",
		"Metrics/Reporting/get_mttx",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("operations = %v, want %v", got, want)
	}
}

func TestParseOperationBasics(t *testing.T) {
	op := findOp(t, loadFixture(t), "delete_incident_role")

	if op.Method != "DELETE" {
		t.Errorf("Method = %q, want DELETE", op.Method)
	}
	if op.Path != "/v1/incidents/{incident_id}/roles/{role_id}" {
		t.Errorf("Path = %q", op.Path)
	}
	if op.Summary != "Delete an incident role" || op.Description != "Remove a role assignment" {
		t.Errorf("Summary/Description = %q / %q", op.Summary, op.Description)
	}
	if op.HasBody {
		t.Error("DELETE with no body params should have HasBody=false")
	}
}

func TestParsePathParamsFollowTemplateOrder(t *testing.T) {
	op := findOp(t, loadFixture(t), "get_incident_role")

	if got := names(op.PathParams); !reflect.DeepEqual(got, []string{"incident_id", "role_id"}) {
		t.Errorf("PathParams = %v, want [incident_id role_id]", got)
	}
}

func TestParseQueryParams(t *testing.T) {
	op := findOp(t, loadFixture(t), "list_incidents")
	q := byName(op.QueryParams)

	if q["page"].Type != "integer" {
		t.Errorf("page type = %q", q["page"].Type)
	}
	if !reflect.DeepEqual(q["status"].Enum, []string{"open", "resolved"}) || q["status"].Description != "Incident status" {
		t.Errorf("status = %+v", q["status"])
	}
	if q["tags"].Type != "array" || q["tags"].ItemType != "string" || !q["tags"].Scalar() {
		t.Errorf("tags = %+v, want scalar string array", q["tags"])
	}
	if op.HasBody {
		t.Error("list_incidents should have HasBody=false")
	}
}

func TestParseBodyResolvesRefsAndClassifiesFields(t *testing.T) {
	op := findOp(t, loadFixture(t), "create_incident")

	if !op.HasBody {
		t.Fatal("create_incident should have HasBody=true")
	}
	want := []string{"customer_impact", "data", "impacts", "labels", "name", "priority", "tag_list"}
	if got := names(op.BodyFields); !reflect.DeepEqual(got, want) {
		t.Fatalf("BodyFields = %v, want %v", got, want)
	}

	f := byName(op.BodyFields)
	if !f["name"].Required || f["name"].Description != "Incident name" || !f["name"].Scalar() {
		t.Errorf("name = %+v, want required scalar with description", f["name"])
	}
	if f["priority"].Type != "integer" || f["customer_impact"].Type != "number" {
		t.Errorf("priority/customer_impact types = %q/%q", f["priority"].Type, f["customer_impact"].Type)
	}
	if !f["tag_list"].Scalar() {
		t.Error("tag_list (array of strings) should be scalar")
	}
	if f["labels"].Scalar() {
		t.Error("labels (object) should not be scalar")
	}
	if f["impacts"].Type != "array" || f["impacts"].ItemType != "object" || f["impacts"].Scalar() {
		t.Errorf("impacts = %+v, want non-scalar array of object", f["impacts"])
	}
}

func TestParseFormDataBecomesBodyFields(t *testing.T) {
	op := findOp(t, loadFixture(t), "close_incident")

	if !op.HasBody {
		t.Fatal("formData params should set HasBody")
	}
	if got := names(op.BodyFields); !reflect.DeepEqual(got, []string{"milestone", "notify", "fields[label]"}) {
		t.Errorf("BodyFields = %v", got)
	}
	if byName(op.BodyFields)["notify"].Type != "boolean" {
		t.Error("notify should be boolean")
	}
}

func TestParseRejectsInvalidSpecs(t *testing.T) {
	cases := map[string]string{
		"not json":            "<html>oops</html>",
		"no paths":            `{"swagger":"2.0","paths":{}}`,
		"missing operationId": `{"paths":{"/x":{"get":{"tags":["X"]}}}}`,
		"missing tag":         `{"paths":{"/x":{"get":{"operationId":"x"}}}}`,
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseResponseFieldsForObject(t *testing.T) {
	op := findOp(t, loadFixture(t), "get_incident_role")

	if !reflect.DeepEqual(op.ResponseFields, []string{"id", "role", "user"}) {
		t.Errorf("ResponseFields = %v", op.ResponseFields)
	}
	if op.ResponseItemFields != nil {
		t.Errorf("a non-paginated response has no item fields, got %v", op.ResponseItemFields)
	}
}

func TestParseResponseFieldsForPaginatedList(t *testing.T) {
	op := findOp(t, loadFixture(t), "list_incidents")

	if !reflect.DeepEqual(op.ResponseFields, []string{"data", "pagination"}) {
		t.Errorf("ResponseFields = %v", op.ResponseFields)
	}
	if !reflect.DeepEqual(op.ResponseItemFields, []string{"id", "name", "number"}) {
		t.Errorf("ResponseItemFields = %v", op.ResponseItemFields)
	}
}

func TestParseResponseFieldsUse201(t *testing.T) {
	op := findOp(t, loadFixture(t), "create_incident")

	if !reflect.DeepEqual(op.ResponseFields, []string{"id", "name", "number"}) {
		t.Errorf("ResponseFields = %v", op.ResponseFields)
	}
}

func TestParseResponseFieldsAbsentWhenUndocumented(t *testing.T) {
	op := findOp(t, loadFixture(t), "close_incident")

	if op.ResponseFields != nil || op.ResponseItemFields != nil {
		t.Errorf("close_incident documents no response, got %v / %v", op.ResponseFields, op.ResponseItemFields)
	}
}
