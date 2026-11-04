package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serveSpec(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../spec/testdata/mini.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSpecUpdateWritesOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	ts := serveSpec(t, http.StatusOK, fixtureBytes(t))

	out, _, err := runCmdFull(t, ts.URL, "spec", "update", "--url", ts.URL)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	path := filepath.Join(dir, "fhcli", "spec.json")
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("override not written: %v", err)
	}
	if !bytes.Equal(written, fixtureBytes(t)) {
		t.Error("override content differs from the download")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if result["source"] != path || result["operations"] != float64(7) || result["fetched_at"] == nil {
		t.Errorf("result = %v", result)
	}
}

func TestSpecUpdateKeepsExistingOnInvalidDownload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "fhcli", "spec.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, fixtureBytes(t), 0o644)
	ts := serveSpec(t, http.StatusOK, []byte("<html>maintenance</html>"))

	_, stderr, err := runCmdFull(t, ts.URL, "spec", "update", "--url", ts.URL)

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "keeping the current one") {
		t.Errorf("stderr = %q", stderr)
	}
	if kept, _ := os.ReadFile(path); !bytes.Equal(kept, fixtureBytes(t)) {
		t.Error("existing override must be left untouched")
	}
}

func TestSpecUpdateFailsOnNon200(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ts := serveSpec(t, http.StatusInternalServerError, []byte("boom"))

	_, stderr, err := runCmdFull(t, ts.URL, "spec", "update", "--url", ts.URL)

	if err == nil {
		t.Error("expected an error")
	}
	if !strings.Contains(stderr, "500") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSpecInfoReportsEmbedded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, _, err := runCmdFull(t, "http://127.0.0.1:1", "spec", "info")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var info map[string]any
	json.Unmarshal([]byte(out), &info)
	if info["source"] != "embedded" {
		t.Errorf("source = %v", info["source"])
	}
	if ops, _ := info["operations"].(float64); ops < 400 {
		t.Errorf("operations = %v", info["operations"])
	}
	if _, present := info["modified_at"]; present {
		t.Error("embedded spec should not report modified_at")
	}
}

func TestSpecInfoReportsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "fhcli", "spec.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, fixtureBytes(t), 0o644)

	out, _, err := runCmdFull(t, "http://127.0.0.1:1", "spec", "info")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var info map[string]any
	json.Unmarshal([]byte(out), &info)
	if info["source"] != path || info["operations"] != float64(7) || info["tags"] != float64(3) || info["modified_at"] == nil {
		t.Errorf("info = %v", info)
	}
}
