package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

const (
	keychainService    = "fhcli"
	errSecItemNotFound = 44
	// security -i reads each command into a fixed-size line buffer.
	maxInteractiveLine = 4000
	securityPath       = "/usr/bin/security"
)

var safeRegion = regexp.MustCompile(`^[a-z0-9-]+$`)

// securityRunner runs /usr/bin/security with args, feeding stdin, and
// returns stdout. A non-zero exit is a *securityError.
type securityRunner func(ctx context.Context, stdin string, args ...string) ([]byte, error)

type securityError struct {
	Code   int
	Stderr string
}

func (e *securityError) Error() string {
	return fmt.Sprintf("security exited %d: %s", e.Code, strings.TrimSpace(e.Stderr))
}

func runSecurity(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, securityPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, &securityError{Code: ee.ExitCode(), Stderr: stderr.String()}
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func isNotFound(err error) bool {
	var se *securityError
	return errors.As(err, &se) && se.Code == errSecItemNotFound
}

// keychainStore keeps each region's login as a generic password (service
// "fhcli", account <region>) in the login Keychain. Items created by
// /usr/bin/security are readable by it without a prompt, so shells started
// by other tools (e.g. Claude Code) can use the login too.
type keychainStore struct {
	run securityRunner
}

// NewKeychainStore returns a Store backed by the macOS login Keychain.
func NewKeychainStore() Store { return keychainStore{run: runSecurity} }

func (s keychainStore) Load(region string) (*Credentials, error) {
	out, err := s.run(context.Background(), "", "find-generic-password", "-s", keychainService, "-a", region, "-w")
	if isNotFound(err) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("reading login from the macOS Keychain: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, ErrCorruptLogin
	}
	return decodeCredentials(data)
}

func (s keychainStore) Save(c *Credentials) error {
	if !safeRegion.MatchString(c.Region) {
		return fmt.Errorf("invalid region %q", c.Region)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", keychainService, c.Region, base64.StdEncoding.EncodeToString(data))
	if len(line) > maxInteractiveLine {
		return errors.New("login is too large for the macOS Keychain; set FHCLI_TOKEN_STORE=file to store it in a file instead")
	}
	if _, err := s.run(context.Background(), line, "-i"); err != nil {
		return fmt.Errorf("saving login to the macOS Keychain: %w; set FHCLI_TOKEN_STORE=file to store it in a file instead", err)
	}
	// security -i can exit 0 when the command inside it failed.
	if got, err := s.Load(c.Region); err != nil || got.AccessToken != c.AccessToken {
		return errors.New("saving login to the macOS Keychain failed; set FHCLI_TOKEN_STORE=file to store it in a file instead")
	}
	return nil
}

func (s keychainStore) Delete(region string) error {
	_, err := s.run(context.Background(), "", "delete-generic-password", "-s", keychainService, "-a", region)
	if err == nil || isNotFound(err) {
		return nil
	}
	return fmt.Errorf("deleting login from the macOS Keychain: %w", err)
}
