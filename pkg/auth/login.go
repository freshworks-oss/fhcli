package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/oauth2"
)

// DefaultPorts are the loopback ports registered as redirect URIs on the
// "FireHydrant CLI" OAuth application. Doorkeeper matches localhost
// redirects by exact port, so login can only use these.
var DefaultPorts = []int{8085, 8086, 8087}

var scopes = []string{"read", "write"}

// httpClient is used for every OAuth and identity call, so none can hang.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// LoginOptions tune Login. Zero values give the defaults.
type LoginOptions struct {
	Ports          []int  // default DefaultPorts
	OrganizationID string // preselects the organization on the consent screen
	// OpenBrowser opens the authorize URL. Nil means only print it.
	OpenBrowser func(url string) error
	Prompt      io.Writer     // where the URL is printed; default io.Discard
	Timeout     time.Duration // default 5 minutes
}

func oauthConfig(r Region) *oauth2.Config {
	return &oauth2.Config{
		ClientID: r.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:   strings.TrimRight(r.AppHost, "/") + "/oauth/authorize",
			TokenURL:  strings.TrimRight(r.AppHost, "/") + "/oauth/token",
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: scopes,
	}
}

// Login runs the authorization code + PKCE flow: it listens on a loopback
// port, sends the user to the region's authorize page, waits for the
// redirect, exchanges the code, and looks up who the token belongs to.
func Login(ctx context.Context, region Region, opts LoginOptions) (*Credentials, error) {
	ports := opts.Ports
	if len(ports) == 0 {
		ports = DefaultPorts
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	prompt := opts.Prompt
	if prompt == nil {
		prompt = io.Discard
	}

	listeners, port, err := listen(ports)
	if err != nil {
		return nil, err
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()
	conf := oauthConfig(region)
	conf.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", port)
	authOpts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if opts.OrganizationID != "" {
		authOpts = append(authOpts, oauth2.SetAuthURLParam("organization_id", opts.OrganizationID))
	}
	authURL := conf.AuthCodeURL(state, authOpts...)

	results := make(chan callbackResult, 1)
	srv := &http.Server{Handler: callbackHandler(state, results), ReadHeaderTimeout: 10 * time.Second}
	for _, l := range listeners {
		go srv.Serve(l)
	}
	// Shutdown, not Close: let the callback finish writing its page, so the
	// browser shows the outcome rather than a reset connection.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(prompt, "Open this URL to log in to FireHydrant:\n\n  %s\n\n", authURL)
	if opts.OpenBrowser != nil {
		if err := opts.OpenBrowser(authURL); err != nil {
			fmt.Fprintf(prompt, "Couldn't open a browser (%v); open the URL above yourself.\n", err)
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var res callbackResult
	select {
	case res = <-results:
	case <-ctx.Done():
		return nil, errors.New("login cancelled")
	case <-timer.C:
		return nil, fmt.Errorf("login timed out after %s", timeout)
	}
	if res.err != nil {
		return nil, res.err
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	tok, err := conf.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, oauthError("login", err)
	}
	id, err := fetchIdentity(ctx, region.APIHost, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		Expiry:         tok.Expiry,
		Region:         region.Name,
		AppHost:        region.AppHost,
		APIHost:        region.APIHost,
		ClientID:       region.ClientID,
		User:           id.Name,
		Email:          id.Email,
		Organization:   id.OrganizationName,
		OrganizationID: id.OrganizationID,
	}, nil
}

// listen binds the first port free on 127.0.0.1, also binding [::1] when the
// host has IPv6 loopback, since browsers may resolve localhost to either.
func listen(ports []int) ([]net.Listener, int, error) {
	for _, p := range ports {
		v4, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		listeners := []net.Listener{v4}
		v6, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", p))
		switch {
		case err == nil:
			listeners = append(listeners, v6)
		case errors.Is(err, syscall.EADDRINUSE):
			v4.Close()
			continue
		}
		return listeners, p, nil
	}
	names := make([]string, len(ports))
	for i, p := range ports {
		names[i] = fmt.Sprint(p)
	}
	return nil, 0, fmt.Errorf("ports %s are in use; free one and retry", strings.Join(names, ", "))
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type callbackResult struct {
	code string
	err  error
}

// callbackHandler serves GET /callback, delivering the first result for this
// login's state and telling the browser the outcome. A callback with any
// other state (a stale tab, or a page probing the port) is turned away
// without ending the login. Other paths (e.g. /favicon.ico) get 404.
func callbackHandler(state string, results chan<- callbackResult) http.Handler {
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if q.Get("state") != state {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, page("This callback doesn't match the login in progress. Return to your terminal."))
			return
		}
		res := callbackResult{code: q.Get("code")}
		switch {
		case q.Get("error") != "":
			msg := q.Get("error")
			if d := q.Get("error_description"); d != "" {
				msg += ": " + d
			}
			res = callbackResult{err: errors.New("login failed: " + msg)}
		case res.code == "":
			res = callbackResult{err: errors.New("login failed: callback had no code")}
		}
		delivered := false
		once.Do(func() {
			results <- res
			delivered = true
		})

		switch {
		case !delivered:
			fmt.Fprint(w, page("This login was already handled. Return to your terminal."))
		case res.err != nil:
			fmt.Fprint(w, page(html.EscapeString(res.err.Error())))
		default:
			fmt.Fprint(w, page("Authorization received. Return to your terminal; you can close this tab."))
		}
	})
	return mux
}

func page(msg string) string {
	return `<!doctype html><meta charset="utf-8"><title>fhcli login</title><body style="font-family:system-ui;margin:4em"><p>` + msg + `</p></body>`
}

// Identity is the /v1/current_user response fields fhcli uses.
type Identity struct {
	Name             string `json:"name"`
	Email            string `json:"email"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
}

func fetchIdentity(ctx context.Context, apiHost, accessToken string) (*Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiHost, "/")+"/v1/current_user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("looking up the logged-in user: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up the logged-in user: GET /v1/current_user returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var id Identity
	if err := json.Unmarshal(body, &id); err != nil {
		return nil, fmt.Errorf("looking up the logged-in user: %w", err)
	}
	return &id, nil
}

// oauthError turns an oauth2 error into "<action> failed: <error>: <description>".
func oauthError(action string, err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode != "" {
			msg := re.ErrorCode
			if re.ErrorDescription != "" {
				msg += ": " + re.ErrorDescription
			}
			return fmt.Errorf("%s failed: %s", action, msg)
		}
		return fmt.Errorf("%s failed: HTTP %d: %s", action, re.Response.StatusCode, strings.TrimSpace(string(re.Body)))
	}
	return fmt.Errorf("%s failed: %w", action, err)
}
