package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// fakeSecurity stands in for /usr/bin/security, keeping items in memory.
type fakeSecurity struct {
	items      map[string]string // account -> base64 secret
	calls      [][]string
	ignoreAdds bool
}

func newFakeSecurity() *fakeSecurity { return &fakeSecurity{items: map[string]string{}} }

func (f *fakeSecurity) run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	switch {
	case len(args) == 1 && args[0] == "-i":
		// add-generic-password -U -s fhcli -a <region> -w <secret>
		fields := strings.Fields(stdin)
		if len(fields) != 8 || fields[0] != "add-generic-password" || fields[2] != "-s" || fields[3] != "fhcli" {
			return nil, &securityError{Code: 1, Stderr: "bad command: " + stdin}
		}
		if !f.ignoreAdds {
			f.items[fields[5]] = fields[7]
		}
		return nil, nil
	case args[0] == "find-generic-password":
		v, ok := f.items[args[4]]
		if !ok {
			return nil, &securityError{Code: errSecItemNotFound}
		}
		return []byte(v + "\n"), nil
	case args[0] == "delete-generic-password":
		if _, ok := f.items[args[4]]; !ok {
			return nil, &securityError{Code: errSecItemNotFound}
		}
		delete(f.items, args[4])
		return nil, nil
	}
	return nil, &securityError{Code: 1, Stderr: "unexpected call"}
}

func TestKeychainRoundTrip(t *testing.T) {
	fake := newFakeSecurity()
	s := keychainStore{run: fake.run}

	if err := s.Save(testCredentials()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("us")

	if err != nil {
		t.Fatal(err)
	}
	if *got != *testCredentials() {
		t.Errorf("Load = %+v", got)
	}
	if want := []string{"find-generic-password", "-s", "fhcli", "-a", "us", "-w"}; strings.Join(fake.calls[len(fake.calls)-1], " ") != strings.Join(want, " ") {
		t.Errorf("last call = %v, want %v", fake.calls[len(fake.calls)-1], want)
	}
}

func TestKeychainKeepsTokensOutOfArgs(t *testing.T) {
	fake := newFakeSecurity()
	s := keychainStore{run: fake.run}

	s.Save(testCredentials())

	for _, call := range fake.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "at-1") || strings.Contains(joined, "rt-1") || strings.Contains(joined, base64.StdEncoding.EncodeToString([]byte("at-1"))) {
			t.Errorf("token material in argv: %v", call)
		}
	}
}

func TestKeychainMissing(t *testing.T) {
	_, err := keychainStore{run: newFakeSecurity().run}.Load("us")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestKeychainCorrupt(t *testing.T) {
	fake := newFakeSecurity()
	fake.items["us"] = "!!!not-base64"

	_, err := keychainStore{run: fake.run}.Load("us")

	if !errors.Is(err, ErrCorruptLogin) {
		t.Errorf("err = %v, want ErrCorruptLogin", err)
	}
}

func TestKeychainDelete(t *testing.T) {
	fake := newFakeSecurity()
	s := keychainStore{run: fake.run}
	s.Save(testCredentials())

	if err := s.Delete("us"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("us"); err != nil {
		t.Errorf("deleting a missing item should succeed, got %v", err)
	}
}

func TestKeychainSaveVerifiesWrite(t *testing.T) {
	fake := newFakeSecurity()
	fake.ignoreAdds = true

	err := keychainStore{run: fake.run}.Save(testCredentials())

	if err == nil || !strings.Contains(err.Error(), "FHCLI_TOKEN_STORE=file") {
		t.Errorf("err = %v, want a failed-save error suggesting FHCLI_TOKEN_STORE=file", err)
	}
}

func TestKeychainRejectsOversizedLogin(t *testing.T) {
	c := testCredentials()
	c.AccessToken = strings.Repeat("x", 4000)

	err := keychainStore{run: newFakeSecurity().run}.Save(c)

	if err == nil || !strings.Contains(err.Error(), "FHCLI_TOKEN_STORE=file") {
		t.Errorf("err = %v, want a too-large error suggesting FHCLI_TOKEN_STORE=file", err)
	}
}

func TestKeychainRejectsUnsafeRegion(t *testing.T) {
	c := testCredentials()
	c.Region = "us -w evil"

	if err := (keychainStore{run: newFakeSecurity().run}).Save(c); err == nil {
		t.Error("a region with spaces must be rejected before building the security command")
	}
}
