package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestApplySelectProjectsNestedAndArrayPaths(t *testing.T) {
	data := json.RawMessage(`{"data":[{"id":"a","owner":{"name":"db"},"x":1},{"id":"b","owner":{"name":"web"}}]}`)

	got, err := applySelect(data, "data.id, data.owner.name,missing")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"data.id":         []any{"a", "b"},
		"data.owner.name": []any{"db", "web"},
		"missing":         nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplySelectProjectsTopLevelArrayPerElement(t *testing.T) {
	got, err := applySelect([]map[string]any{{"id": 1, "n": "x"}, {"id": 2}}, "id")
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"id": float64(1)}, map[string]any{"id": float64(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplySelectRejectsEmptySelector(t *testing.T) {
	if _, err := applySelect(map[string]any{}, " , "); err == nil {
		t.Error("an empty --select must be an error")
	}
}

func TestUsersFindPrefersExactEmailMatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/users" || r.URL.Query().Get("query") != "robert@x.com" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write([]byte(`{"data":[{"id":"u1","name":"Robert Ross","email":"robert@x.com","extra":1},{"id":"u2","name":"Roberta","email":"roberta@x.com"}]}`))
	}))
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "users", "find", "robert@x.com")

	if err != nil {
		t.Fatal(err)
	}
	want := `{"exact_match":true,"users":[{"id":"u1","name":"Robert Ross","email":"robert@x.com"}]}`
	if strings.TrimSpace(out) != want {
		t.Errorf("out = %s, want %s", out, want)
	}
}

func TestUsersFindListsCandidatesWithoutExactMatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"u1","name":"Robert Ross","email":"a@x.com"},{"id":"u2","name":"Roberta","email":"b@x.com"}]}`))
	}))
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "users", "find", "rob")

	if err != nil || !strings.Contains(out, `"exact_match":false`) || !strings.Contains(out, "u2") {
		t.Errorf("err = %v, out = %s; want both candidates", err, out)
	}
}

func TestSelectFlagProjectsCommandOutput(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"u1","name":"A"}]}`))
	}))
	defer ts.Close()

	out, err := runCmd(t, ts.URL, "api", "users", "list-users", "--select", "data.id")

	if err != nil || strings.TrimSpace(out) != `{"data.id":["u1"]}` {
		t.Errorf("err = %v, out = %s", err, out)
	}
}

func TestServerErrorOnCreateWarnsAboutDuplicates(t *testing.T) {
	ts := statusServer(t, http.StatusInternalServerError, 0)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "incidents", "create-incident-note", "inc-1", "--body", "x")

	if err == nil || !strings.Contains(stderr, "before retrying") {
		t.Errorf("err = %v, stderr = %s; want a duplicate warning", err, stderr)
	}
}

func TestServerErrorOnReadGetsNoDuplicateWarning(t *testing.T) {
	ts := statusServer(t, http.StatusInternalServerError, 0)

	_, stderr, _ := runCmdFull(t, ts.URL, "api", "alerts", "list-alerts")

	if strings.Contains(stderr, "before retrying") {
		t.Errorf("stderr = %s; reads are safe to retry", stderr)
	}
}
