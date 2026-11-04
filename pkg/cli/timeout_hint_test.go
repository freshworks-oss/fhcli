package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func statusServer(t *testing.T, status int, delay time.Duration) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(`{"error":"request_timeout"}`))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestGatewayTimeoutHintsAtDateFilters(t *testing.T) {
	ts := statusServer(t, http.StatusGatewayTimeout, 0)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "alerts", "list-alerts", "--query", "Temporal backlog")

	if err == nil {
		t.Fatal("a 504 must exit non-zero")
	}
	for _, want := range []string{"504", "timed out", "--start-datetime", "--end-datetime"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %s, want %q", stderr, want)
		}
	}
}

func TestGatewayTimeoutHintWithoutDateFilters(t *testing.T) {
	ts := statusServer(t, http.StatusGatewayTimeout, 0)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "incidents", "create-incident-note", "inc-1", "--body", "x")

	if err == nil || !strings.Contains(stderr, "timed out") || strings.Contains(stderr, "--start-datetime") {
		t.Errorf("err = %v, stderr = %s; want a generic timeout hint", err, stderr)
	}
}

func TestClientTimeoutGetsTheHint(t *testing.T) {
	ts := statusServer(t, http.StatusOK, 300*time.Millisecond)
	old := apiHTTPClient.Timeout
	apiHTTPClient.Timeout = 50 * time.Millisecond
	t.Cleanup(func() { apiHTTPClient.Timeout = old })

	_, stderr, err := runCmdFull(t, ts.URL, "api", "alerts", "list-alerts")

	if err == nil || !strings.Contains(stderr, "timed out") || !strings.Contains(stderr, "--start-datetime") {
		t.Errorf("err = %v, stderr = %s; a client-side timeout should get the same hint", err, stderr)
	}
}

func TestOtherErrorsGetNoTimeoutHint(t *testing.T) {
	ts := statusServer(t, http.StatusInternalServerError, 0)

	_, stderr, err := runCmdFull(t, ts.URL, "api", "alerts", "list-alerts")

	if err == nil || strings.Contains(stderr, "timed out") {
		t.Errorf("err = %v, stderr = %s; a 500 is not a timeout", err, stderr)
	}
}
