package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRawClientDoSendsJSONBody(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotContentType, gotAuth string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotContentType, gotAuth = r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"inc-1"}`))
	}))
	defer ts.Close()
	client := &rawClient{baseURL: ts.URL, token: "tok", http: ts.Client()}

	out, err := client.do(context.Background(), http.MethodPost, "/v1/incidents", url.Values{"page": {"2"}}, []byte(`{"name":"DB down"}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"id":"inc-1"}` {
		t.Errorf("body = %s", out)
	}
	if gotMethod != "POST" || gotPath != "/v1/incidents" || gotQuery != "page=2" {
		t.Errorf("request = %s %s?%s", gotMethod, gotPath, gotQuery)
	}
	if gotContentType != "application/json" || gotAuth != "tok" {
		t.Errorf("headers: Content-Type=%q Authorization=%q", gotContentType, gotAuth)
	}
	if gotBody["name"] != "DB down" {
		t.Errorf("sent body = %v", gotBody)
	}
}

func TestRawClientDoEmptySuccessBodyIsEmptyObject(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	client := &rawClient{baseURL: ts.URL, token: "tok", http: ts.Client()}

	out, err := client.do(context.Background(), http.MethodDelete, "/v1/incidents/x/roles/y", nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("body = %q, want {}", out)
	}
}

func TestRawClientDoNon2xxIncludesStatusAndBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"detail":"severity is invalid"}`))
	}))
	defer ts.Close()
	client := &rawClient{baseURL: ts.URL, token: "tok", http: ts.Client()}

	_, err := client.do(context.Background(), http.MethodPatch, "/v1/incidents/x", nil, []byte(`{}`))

	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"PATCH /v1/incidents/x", "422", "severity is invalid"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestRawClientDoRejectsNonJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>maintenance</html>"))
	}))
	defer ts.Close()
	client := &rawClient{baseURL: ts.URL, token: "tok", http: ts.Client()}

	if _, err := client.do(context.Background(), http.MethodGet, "/v1/ping", nil, nil); err == nil {
		t.Fatal("expected an error for a non-JSON body")
	}
}
