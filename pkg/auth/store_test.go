package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCredentials() *Credentials {
	return &Credentials{
		AccessToken:    "at-1",
		RefreshToken:   "rt-1",
		Expiry:         time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
		Region:         "us",
		AppHost:        "https://app.firehydrant.io",
		APIHost:        "https://api.firehydrant.io",
		ClientID:       "cid",
		User:           "Dylan",
		Email:          "dylan@example.com",
		Organization:   "FireHydrant",
		OrganizationID: "org-1",
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials")
	s := NewFileStore(dir)

	if err := s.Save(testCredentials()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("us")

	if err != nil {
		t.Fatal(err)
	}
	if *got != *testCredentials() {
		t.Errorf("Load = %+v, want %+v", got, testCredentials())
	}
}

func TestFileStorePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials")
	s := NewFileStore(dir)

	if err := s.Save(testCredentials()); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(filepath.Join(dir, "us.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want only us.json (no temp files left)", len(entries))
	}
}

func TestFileStoreMissing(t *testing.T) {
	_, err := NewFileStore(t.TempDir()).Load("us")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestFileStoreCorrupt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "us.json"), []byte("{not json"), 0o600)

	_, err := NewFileStore(dir).Load("us")

	if !errors.Is(err, ErrCorruptLogin) || !strings.Contains(err.Error(), "fhcli login") {
		t.Errorf("err = %v, want ErrCorruptLogin mentioning fhcli login", err)
	}
}

func TestFileStoreDelete(t *testing.T) {
	s := NewFileStore(t.TempDir())
	s.Save(testCredentials())

	if err := s.Delete("us"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("us"); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("after Delete, Load err = %v", err)
	}
	if err := s.Delete("us"); err != nil {
		t.Errorf("deleting a missing login should succeed, got %v", err)
	}
}

func TestFileStoreRejectsEmptyRegion(t *testing.T) {
	c := testCredentials()
	c.Region = ""
	if err := NewFileStore(t.TempDir()).Save(c); err == nil {
		t.Error("Save with no region should fail")
	}
}

func TestCurrentRegion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if got := CurrentRegion(); got != "us" {
		t.Errorf("default CurrentRegion = %q, want us", got)
	}
	if err := SetCurrentRegion("eu"); err != nil {
		t.Fatal(err)
	}
	if got := CurrentRegion(); got != "eu" {
		t.Errorf("CurrentRegion = %q, want eu", got)
	}
	fi, _ := os.Stat(filepath.Join(ConfigDir(), "config.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config.json mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestConfigDirDefaultsToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if got := ConfigDir(); got != filepath.Join(home, ".config", "fhcli") {
		t.Errorf("ConfigDir() = %s", got)
	}
}

func TestDefaultStoreHonorsFileOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("FHCLI_TOKEN_STORE", "file")

	s, ok := DefaultStore().(fileStore)
	if !ok || s.dir != filepath.Join(ConfigDir(), "credentials") {
		t.Errorf("DefaultStore() = %#v, want fileStore in ConfigDir()/credentials", DefaultStore())
	}
}
