package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// storeWith returns a file store in a temp XDG_CONFIG_HOME holding c.
func storeWith(t *testing.T, c *Credentials) Store {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := NewFileStore(filepath.Join(ConfigDir(), "credentials"))
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	return s
}

func loginFor(f *fakeOAuth, expiry time.Time) *Credentials {
	return &Credentials{
		AccessToken: "at-1", RefreshToken: "rt-1", Expiry: expiry,
		Region: "us", AppHost: f.URL, APIHost: f.URL, ClientID: "cid",
	}
}

func TestTokenFreshIsReturnedAsIs(t *testing.T) {
	f := newFakeOAuth(t)
	s := storeWith(t, loginFor(f, time.Now().Add(time.Hour)))

	c, err := Token(context.Background(), s, "us")

	if err != nil || c.AccessToken != "at-1" || f.refreshCount != 0 {
		t.Errorf("c = %+v, err = %v, refreshes = %d; want the stored token, no refresh", c, err, f.refreshCount)
	}
}

func TestTokenZeroExpiryIsFresh(t *testing.T) {
	f := newFakeOAuth(t)
	s := storeWith(t, loginFor(f, time.Time{}))

	c, err := Token(context.Background(), s, "us")

	if err != nil || c.AccessToken != "at-1" || f.refreshCount != 0 {
		t.Errorf("a token with no expiry should not be refreshed on every call (refreshes = %d, err = %v)", f.refreshCount, err)
	}
}

func TestTokenRefreshesWhenExpiring(t *testing.T) {
	f := newFakeOAuth(t)
	s := storeWith(t, loginFor(f, time.Now().Add(30*time.Second)))

	c, err := Token(context.Background(), s, "us")

	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "at-2" || c.RefreshToken != "rt-2" || time.Until(c.Expiry) < time.Hour {
		t.Errorf("refreshed credentials = %+v", c)
	}
	stored, _ := s.Load("us")
	if stored.AccessToken != "at-2" || stored.RefreshToken != "rt-2" {
		t.Errorf("rotated tokens not saved: %+v", stored)
	}
}

func TestTokenInvalidGrantMeansLoginExpired(t *testing.T) {
	f := newFakeOAuth(t)
	f.refreshInvalid = true
	s := storeWith(t, loginFor(f, time.Now().Add(-time.Minute)))

	_, err := Token(context.Background(), s, "us")

	if !errors.Is(err, ErrLoginExpired) {
		t.Errorf("err = %v, want ErrLoginExpired", err)
	}
}

func TestTokenFailedRefreshKeepsUnexpiredToken(t *testing.T) {
	f := newFakeOAuth(t)
	f.refreshDown = true
	s := storeWith(t, loginFor(f, time.Now().Add(30*time.Second)))

	c, err := Token(context.Background(), s, "us")

	if err != nil || c.AccessToken != "at-1" {
		t.Errorf("c = %+v, err = %v; a failed early refresh should fall back to the still-valid token", c, err)
	}
}

func TestTokenFailedRefreshOfExpiredTokenIsAnError(t *testing.T) {
	f := newFakeOAuth(t)
	f.refreshDown = true
	s := storeWith(t, loginFor(f, time.Now().Add(-time.Minute)))

	_, err := Token(context.Background(), s, "us")

	if err == nil || errors.Is(err, ErrLoginExpired) {
		t.Errorf("err = %v, want a refresh error (not login expired)", err)
	}
}

func TestTokenNotLoggedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := Token(context.Background(), NewFileStore(t.TempDir()), "us")

	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestTokenConcurrentCallsRefreshOnce(t *testing.T) {
	f := newFakeOAuth(t)
	s := storeWith(t, loginFor(f, time.Now().Add(-time.Minute)))

	var wg sync.WaitGroup
	tokens := make([]string, 4)
	errs := make([]error, 4)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := Token(context.Background(), s, "us")
			errs[i] = err
			if c != nil {
				tokens[i] = c.AccessToken
			}
		}(i)
	}
	wg.Wait()

	for i := range tokens {
		if errs[i] != nil || tokens[i] != "at-2" {
			t.Errorf("call %d: token %q, err %v; want at-2", i, tokens[i], errs[i])
		}
	}
	if f.refreshCount != 1 {
		t.Errorf("refreshCount = %d, want exactly 1", f.refreshCount)
	}
}

func TestRevokeSendsBothTokens(t *testing.T) {
	f := newFakeOAuth(t)

	Revoke(context.Background(), loginFor(f, time.Now()))

	if len(f.revoked) != 2 {
		t.Fatalf("revoke calls = %d, want 2", len(f.revoked))
	}
	if f.revoked[0].Get("token") != "at-1" || f.revoked[1].Get("token") != "rt-1" {
		t.Errorf("revoked = %v", f.revoked)
	}
	for _, form := range f.revoked {
		if form.Get("client_id") != "cid" {
			t.Errorf("revoke without client_id: %v", form)
		}
	}
}

func TestRevokeIgnoresUnreachableServer(t *testing.T) {
	c := &Credentials{AccessToken: "a", RefreshToken: "r", AppHost: "http://127.0.0.1:1", ClientID: "cid"}
	Revoke(context.Background(), c) // must return without panicking
}
