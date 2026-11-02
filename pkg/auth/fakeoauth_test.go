package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// fakeOAuth mimics the parts of FireHydrant's Doorkeeper server and public
// API that login, refresh, and revoke use.
type fakeOAuth struct {
	*httptest.Server

	mu             sync.Mutex
	authorizeQuery url.Values
	challenge      string
	deny           bool // authorize redirects with error=access_denied
	wrongState     bool // authorize redirects with a different state
	failExchange   bool // /oauth/token rejects the code
	refreshInvalid bool // /oauth/token rejects refresh with invalid_grant
	refreshDown    bool // /oauth/token answers refresh with a 503
	omitExpiresIn  bool // token responses have no expires_in
	refreshCount   int
	refreshToken   string // the refresh token the server currently accepts
	accessToken    string // the access token the server currently accepts
	revoked        []url.Values
}

func newFakeOAuth(t *testing.T) *fakeOAuth {
	t.Helper()
	f := &fakeOAuth{refreshToken: "rt-1", accessToken: "at-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/authorize", f.authorize)
	mux.HandleFunc("POST /oauth/token", f.token)
	mux.HandleFunc("POST /oauth/revoke", f.revoke)
	mux.HandleFunc("GET /v1/current_user", f.currentUser)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOAuth) region() Region {
	return Region{Name: "us", AppHost: f.URL, APIHost: f.URL, ClientID: "cid"}
}

func (f *fakeOAuth) authorize(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.authorizeQuery = q
	f.challenge = q.Get("code_challenge")
	target, _ := url.Parse(q.Get("redirect_uri"))
	back := url.Values{}
	switch {
	case f.deny:
		back.Set("error", "access_denied")
		back.Set("error_description", "The resource owner denied the request.")
		back.Set("state", q.Get("state")) // RFC 6749 4.1.2.1; Doorkeeper echoes it
	case f.wrongState:
		back.Set("code", "the-code")
		back.Set("state", "not-"+q.Get("state"))
	default:
		back.Set("code", "the-code")
		back.Set("state", q.Get("state"))
	}
	target.RawQuery = back.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (f *fakeOAuth) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ParseForm()
	if r.Form.Get("client_id") != "cid" {
		oauthErrorResponse(w, "invalid_client", "unknown client")
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if f.failExchange || r.Form.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			oauthErrorResponse(w, "invalid_grant", "The provided authorization grant is invalid.")
			return
		}
		f.writeToken(w, f.accessToken, f.refreshToken)
	case "refresh_token":
		if f.refreshDown {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if f.refreshInvalid || r.Form.Get("refresh_token") != f.refreshToken {
			oauthErrorResponse(w, "invalid_grant", "The provided authorization grant is invalid.")
			return
		}
		f.refreshCount++
		f.accessToken = fmt.Sprintf("at-%d", f.refreshCount+1)
		f.refreshToken = fmt.Sprintf("rt-%d", f.refreshCount+1)
		f.writeToken(w, f.accessToken, f.refreshToken)
	default:
		oauthErrorResponse(w, "unsupported_grant_type", "")
	}
}

func (f *fakeOAuth) writeToken(w http.ResponseWriter, access, refresh string) {
	body := map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "scope": "read write"}
	if !f.omitExpiresIn {
		body["expires_in"] = 7200
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

func oauthErrorResponse(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}

func (f *fakeOAuth) revoke(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ParseForm()
	f.revoked = append(f.revoked, r.PostForm)
	w.Write([]byte("{}"))
}

func (f *fakeOAuth) currentUser(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.accessToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{
		"id": "user-1", "name": "Dylan", "email": "dylan@example.com",
		"organization_id": "org-1", "organization_name": "FireHydrant",
	})
}

// followingBrowser simulates a browser: it GETs the authorize URL and
// follows the redirect to the loopback callback. The request may outlive the
// test (Login can return first, e.g. on access_denied), so it must not touch
// t; a failed request just shows up as Login's result.
func followingBrowser(_ *testing.T) func(string) error {
	return func(u string) error {
		go func() {
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

// freePort returns a TCP port that was free on 127.0.0.1 a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
