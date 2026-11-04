package auth

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLoginSuccess(t *testing.T) {
	f := newFakeOAuth(t)
	port := freePort(t)

	c, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{port}, OpenBrowser: followingBrowser(t)})

	if err != nil {
		t.Fatal(err)
	}
	want := Credentials{
		AccessToken: "at-1", RefreshToken: "rt-1", Region: "us", AppHost: f.URL, APIHost: f.URL, ClientID: "cid",
		User: "Dylan", Email: "dylan@example.com", Organization: "FireHydrant", OrganizationID: "org-1",
	}
	got := *c
	got.Expiry = time.Time{}
	if got != want {
		t.Errorf("credentials = %+v, want %+v", got, want)
	}
	if d := time.Until(c.Expiry); d < 119*time.Minute || d > 121*time.Minute {
		t.Errorf("expiry %v from now, want ~2h", d)
	}
	q := f.authorizeQuery
	for key, val := range map[string]string{
		"client_id":             "cid",
		"response_type":         "code",
		"scope":                 "read write",
		"code_challenge_method": "S256",
		"redirect_uri":          fmt.Sprintf("http://localhost:%d/callback", port),
	} {
		if q.Get(key) != val {
			t.Errorf("authorize %s = %q, want %q", key, q.Get(key), val)
		}
	}
	if len(q.Get("state")) < 32 || q.Get("code_challenge") == "" {
		t.Errorf("authorize state/challenge missing: %v", q)
	}
	if q.Has("organization_id") {
		t.Error("organization_id sent without --org")
	}
}

func TestLoginPassesOrganization(t *testing.T) {
	f := newFakeOAuth(t)

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, OpenBrowser: followingBrowser(t), OrganizationID: "org-9"})

	if err != nil {
		t.Fatal(err)
	}
	if f.authorizeQuery.Get("organization_id") != "org-9" {
		t.Errorf("organization_id = %q", f.authorizeQuery.Get("organization_id"))
	}
}

func TestLoginFallsBackToNextPort(t *testing.T) {
	f := newFakeOAuth(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port
	next := freePort(t)

	_, err = Login(context.Background(), f.region(), LoginOptions{Ports: []int{busyPort, next}, OpenBrowser: followingBrowser(t)})

	if err != nil {
		t.Fatal(err)
	}
	if got := f.authorizeQuery.Get("redirect_uri"); got != fmt.Sprintf("http://localhost:%d/callback", next) {
		t.Errorf("redirect_uri = %s, want the second port", got)
	}
}

func TestLoginAllPortsBusy(t *testing.T) {
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	_, err := Login(context.Background(), Region{Name: "us", AppHost: "http://127.0.0.1:1", ClientID: "cid"}, LoginOptions{Ports: []int{port}})

	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Errorf("err = %v, want ports-in-use error", err)
	}
}

func TestLoginAccessDenied(t *testing.T) {
	f := newFakeOAuth(t)
	f.deny = true

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, OpenBrowser: followingBrowser(t)})

	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "denied the request") {
		t.Errorf("err = %v, want access_denied with description", err)
	}
}

func TestLoginIgnoresCallbackWithWrongState(t *testing.T) {
	f := newFakeOAuth(t)
	port := freePort(t)
	var strayStatus int
	browser := func(u string) error {
		go func() {
			// A stale tab or a probing page hits the callback first.
			stray := fmt.Sprintf("http://localhost:%d/callback?code=x&state=stale", port)
			if resp, err := http.Get(stray); err == nil {
				strayStatus = resp.StatusCode
				resp.Body.Close()
			}
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	c, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{port}, OpenBrowser: browser})

	if err != nil || c == nil {
		t.Fatalf("a callback with the wrong state ended the login: %v", err)
	}
	if strayStatus != http.StatusBadRequest {
		t.Errorf("stray callback status = %d, want 400", strayStatus)
	}
}

func TestLoginWithOnlyWrongStateCallbackTimesOut(t *testing.T) {
	f := newFakeOAuth(t)
	f.wrongState = true

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, OpenBrowser: followingBrowser(t), Timeout: 300 * time.Millisecond})

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want the login to keep waiting (and time out) rather than accept a wrong state", err)
	}
}

func TestLoginExchangeError(t *testing.T) {
	f := newFakeOAuth(t)
	f.failExchange = true

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, OpenBrowser: followingBrowser(t)})

	if err == nil || !strings.Contains(err.Error(), "invalid_grant") || !strings.Contains(err.Error(), "authorization grant is invalid") {
		t.Errorf("err = %v, want the server's error and description", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	f := newFakeOAuth(t)

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, Timeout: 50 * time.Millisecond})

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want timeout", err)
	}
}

func TestLoginCancelled(t *testing.T) {
	f := newFakeOAuth(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Login(ctx, f.region(), LoginOptions{Ports: []int{freePort(t)}})

	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %v, want cancelled", err)
	}
}

func TestLoginPrintsURL(t *testing.T) {
	f := newFakeOAuth(t)
	var prompt bytes.Buffer

	Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, Prompt: &prompt, Timeout: 50 * time.Millisecond})

	if !strings.Contains(prompt.String(), f.URL+"/oauth/authorize?") {
		t.Errorf("prompt = %q, want the authorize URL", prompt.String())
	}
}

func TestLoginIgnoresStrayRequests(t *testing.T) {
	f := newFakeOAuth(t)
	port := freePort(t)
	browser := func(u string) error {
		go func() {
			if resp, err := http.Get(fmt.Sprintf("http://localhost:%d/favicon.ico", port)); err == nil {
				resp.Body.Close()
			}
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{port}, OpenBrowser: browser})

	if err != nil {
		t.Errorf("a stray /favicon.ico request broke login: %v", err)
	}
}

func TestLoginAcceptsIPv6Callback(t *testing.T) {
	if l, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback")
	} else {
		l.Close()
	}
	f := newFakeOAuth(t)
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	browser := func(u string) error {
		go func() {
			resp, err := noFollow.Get(u)
			if err != nil {
				return
			}
			resp.Body.Close()
			loc := strings.Replace(resp.Header.Get("Location"), "localhost", "[::1]", 1)
			if resp, err := http.Get(loc); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	_, err := Login(context.Background(), f.region(), LoginOptions{Ports: []int{freePort(t)}, OpenBrowser: browser})

	if err != nil {
		t.Errorf("callback on [::1] failed: %v", err)
	}
}
