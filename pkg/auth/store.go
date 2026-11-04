package auth

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Credentials is a stored login for one region.
type Credentials struct {
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	Expiry         time.Time `json:"expiry"`
	Region         string    `json:"region"`
	AppHost        string    `json:"app_host"`
	APIHost        string    `json:"api_host"`
	ClientID       string    `json:"client_id"`
	User           string    `json:"user"`
	Email          string    `json:"email"`
	Organization   string    `json:"organization"`
	OrganizationID string    `json:"organization_id"`
}

var (
	ErrNotLoggedIn  = errors.New("not logged in; run `fhcli login`")
	ErrCorruptLogin = errors.New("stored login is unreadable; run `fhcli login`")
)

// Store keeps one login per region.
type Store interface {
	// Load returns ErrNotLoggedIn when the region has no stored login.
	Load(region string) (*Credentials, error)
	Save(c *Credentials) error
	// Delete succeeds when there is nothing to delete.
	Delete(region string) error
}

// ConfigDir is $XDG_CONFIG_HOME/fhcli, defaulting to ~/.config/fhcli.
func ConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "fhcli")
}

// DefaultStore returns the Keychain on macOS and a 0600 file store elsewhere.
// FHCLI_TOKEN_STORE=file forces the file store.
func DefaultStore() Store {
	if os.Getenv("FHCLI_TOKEN_STORE") != "file" && runtime.GOOS == "darwin" {
		if _, err := os.Stat(securityPath); err == nil {
			return NewKeychainStore()
		}
	}
	return NewFileStore(filepath.Join(ConfigDir(), "credentials"))
}

type localConfig struct {
	Region string `json:"region"`
}

func localConfigPath() string { return filepath.Join(ConfigDir(), "config.json") }

// CurrentRegion is the region of the last login, defaulting to "us".
func CurrentRegion() string {
	data, err := os.ReadFile(localConfigPath())
	if err != nil {
		return "us"
	}
	var c localConfig
	if json.Unmarshal(data, &c) != nil || c.Region == "" {
		return "us"
	}
	return c.Region
}

// SetCurrentRegion records the region the next commands use.
func SetCurrentRegion(name string) error {
	data, err := json.Marshal(localConfig{Region: name})
	if err != nil {
		return err
	}
	return WriteFileAtomic(localConfigPath(), data)
}

// writeFileAtomic writes data to path with mode 0600, creating the directory
// with mode 0700, via a temp file and rename so readers never see a partial
// file.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func decodeCredentials(data []byte) (*Credentials, error) {
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil || c.RefreshToken == "" {
		return nil, ErrCorruptLogin
	}
	return &c, nil
}

type fileStore struct {
	dir string
}

// NewFileStore stores each region's login in <dir>/<region>.json.
func NewFileStore(dir string) Store { return fileStore{dir: dir} }

func (s fileStore) path(region string) string { return filepath.Join(s.dir, region+".json") }

func (s fileStore) Load(region string) (*Credentials, error) {
	data, err := os.ReadFile(s.path(region))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	return decodeCredentials(data)
}

func (s fileStore) Save(c *Credentials) error {
	if c.Region == "" {
		return errors.New("credentials have no region")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.path(c.Region), data)
}

func (s fileStore) Delete(region string) error {
	err := os.Remove(s.path(region))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
