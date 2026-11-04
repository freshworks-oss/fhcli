package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/oauth2"
)

var ErrLoginExpired = errors.New("login expired; run `fhcli login`")

// refreshMargin refreshes a token this long before it expires, so a request
// never starts with a token about to die.
const refreshMargin = time.Minute

// fresh reports whether the access token can be used as-is. A zero expiry
// means the server gave no lifetime; treat it as valid rather than refresh
// on every command.
func (c *Credentials) fresh() bool {
	return c.Expiry.IsZero() || time.Until(c.Expiry) > refreshMargin
}

// Token returns the region's stored login with a usable access token,
// refreshing (and saving the rotated refresh token) when it is about to
// expire. If that refresh fails other than by the login being revoked, the
// current token is returned while it has not yet expired. Refreshes are
// serialized across fhcli processes with a file lock.
func Token(ctx context.Context, store Store, region string) (*Credentials, error) {
	c, err := store.Load(region)
	if err != nil || c.fresh() {
		return c, err
	}

	unlock, err := lockFile(filepath.Join(ConfigDir(), "refresh.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()

	// Another fhcli process may have refreshed while this one waited.
	if c, err = store.Load(region); err != nil || c.fresh() {
		return c, err
	}

	conf := oauthConfig(Region{AppHost: c.AppHost, ClientID: c.ClientID})
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	tok, err := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: c.RefreshToken}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			return nil, ErrLoginExpired
		}
		// Refreshing early is an optimization: a network blip or server
		// error must not fail a command while the current token still works.
		if time.Now().Before(c.Expiry) {
			return c, nil
		}
		return nil, oauthError("refreshing login", err)
	}
	c.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.RefreshToken = tok.RefreshToken
	}
	c.Expiry = tok.Expiry
	if err := store.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Revoke asks the server to revoke the access and refresh tokens. It is
// best-effort: a token that is already dead needs no revoking.
func Revoke(ctx context.Context, c *Credentials) {
	for _, tok := range []string{c.AccessToken, c.RefreshToken} {
		if tok == "" {
			continue
		}
		form := url.Values{"token": {tok}, "client_id": {c.ClientID}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.AppHost, "/")+"/oauth/revoke", strings.NewReader(form.Encode()))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if resp, err := httpClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}
}
