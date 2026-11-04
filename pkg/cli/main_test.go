package cli

import (
	"os"
	"testing"
)

// TestMain points XDG_CONFIG_HOME at an empty temp dir so a developer's real
// ~/.config/fhcli/spec.json can't change which spec the tests see.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fhcli-test-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	// FHCLI_TEST_SPEC runs the CLI tests against another spec file (for
	// example a freshly fetched live spec) by installing it as the override.
	if path := os.Getenv("FHCLI_TEST_SPEC"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		if err := os.MkdirAll(dir+"/fhcli", 0o700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(dir+"/fhcli/spec.json", data, 0o600); err != nil {
			panic(err)
		}
	}
	// Never read or write the developer's real Keychain login.
	os.Setenv("FHCLI_TOKEN_STORE", "file")
	// Keep the developer's credentials and host out of tests: a real token
	// would make "no token" tests vacuous and could reach the real API.
	for _, name := range []string{"FIREHYDRANT_API_TOKEN", "FH_API_KEY", "FH_API_HOST", "FH_CONFIG_FILE"} {
		os.Unsetenv(name)
	}

	code := m.Run()

	os.RemoveAll(dir)
	os.Exit(code)
}

func TestTestEnvironmentHasNoCredentials(t *testing.T) {
	for _, name := range []string{"FIREHYDRANT_API_TOKEN", "FH_API_KEY", "FH_API_HOST", "FH_CONFIG_FILE"} {
		if v := os.Getenv(name); v != "" {
			t.Errorf("%s leaks into tests from the developer's shell; TestMain must clear it", name)
		}
	}
}
