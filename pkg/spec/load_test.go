package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeOverride(t *testing.T, dir string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, "fhcli", "spec.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOverridePathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/fh-xdg")

	path, err := OverridePath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/fh-xdg/fhcli/spec.json" {
		t.Errorf("OverridePath = %q", path)
	}
}

func TestOverridePathDefaultsToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/fh-home")

	path, err := OverridePath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/fh-home/.config/fhcli/spec.json" {
		t.Errorf("OverridePath = %q", path)
	}
}

func TestLoadUsesEmbeddedWhenNoOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != EmbeddedSource {
		t.Errorf("Source = %q, want %q", s.Source, EmbeddedSource)
	}
	if !s.ModifiedAt.IsZero() {
		t.Error("embedded spec should have zero ModifiedAt")
	}
}

func TestEmbeddedSpecParsesEveryOperation(t *testing.T) {
	var raw struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(embedded, &raw); err != nil {
		t.Fatalf("embedded spec is not JSON: %v", err)
	}
	want := 0
	for _, item := range raw.Paths {
		for _, m := range httpMethods {
			if _, ok := item[m]; ok {
				want++
			}
		}
	}

	s, err := Parse(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != want {
		t.Errorf("parsed %d operations, spec has %d", len(s.Operations), want)
	}
	if want < 400 {
		t.Errorf("embedded spec has only %d operations; did `make spec` fetch the right document?", want)
	}
}

func TestLoadPrefersOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	data, err := os.ReadFile("testdata/mini.json")
	if err != nil {
		t.Fatal(err)
	}
	path := writeOverride(t, dir, data)

	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != path {
		t.Errorf("Source = %q, want %q", s.Source, path)
	}
	if len(s.Operations) != 7 {
		t.Errorf("got %d operations, want 7 from the fixture", len(s.Operations))
	}
	if s.ModifiedAt.IsZero() {
		t.Error("override spec should record ModifiedAt")
	}
}

func TestLoadRejectsInvalidOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeOverride(t, dir, []byte("not json"))

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for an invalid override")
	}
	if !strings.Contains(err.Error(), "run `fhcli spec update` or delete the file") {
		t.Errorf("error should tell the user how to recover, got: %v", err)
	}
}

func TestLoadIgnoresOverrideOlderThanBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	data, err := os.ReadFile("testdata/mini.json")
	if err != nil {
		t.Fatal(err)
	}
	writeOverride(t, dir, data)
	orig := binaryModTime
	t.Cleanup(func() { binaryModTime = orig })
	// The binary was upgraded after the override was fetched.
	binaryModTime = func() time.Time { return time.Now().Add(time.Hour) }

	s, err := Load()

	if err != nil || s.Source != EmbeddedSource {
		t.Errorf("Source = %v, err = %v; an override older than the binary should give way to the embedded spec", s, err)
	}
}
