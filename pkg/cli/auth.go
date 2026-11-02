package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"sync/atomic"
	"time"

	"github.com/firehydrant/fhcli/pkg/auth"
	"github.com/urfave/cli/v3"
)

const (
	authInfoKey ctxKey = "authInfo"
	authErrKey  ctxKey = "authErr"
)

var errNoCredentials = errors.New("API token is required. Run `fhcli login`, or set the --token flag, the FIREHYDRANT_API_TOKEN env var, or a config file")

// authInfo is how this run authenticates.
type authInfo struct {
	source  string // "api_token" or "login"
	header  string // the Authorization header value
	apiHost string
	creds   *auth.Credentials // set when source is "login"
}

// explicitAuth records which auth flags were given by flag or env var, as
// opposed to by a config file.
type explicitAuth struct {
	token, host bool
}

// resolveAuth picks credentials in precedence order: an API token given by
// flag or env var, then the stored login for the current region, then an API
// token from a config file. A config file can sit in world-writable /tmp, so
// it never outranks a login. A login goes to its region's API host unless
// --api-host or FH_API_HOST was given explicitly. It returns nil, nil when
// there are no credentials.
func resolveAuth(ctx context.Context, cmd *cli.Command, explicit explicitAuth) (*authInfo, error) {
	token := cmd.String("token")
	apiToken := &authInfo{source: "api_token", header: token, apiHost: resolveAPIHost(cmd)}
	if token != "" && explicit.token {
		return apiToken, nil
	}
	creds, err := auth.Token(ctx, auth.DefaultStore(), auth.CurrentRegion())
	if errors.Is(err, auth.ErrNotLoggedIn) {
		if token != "" {
			return apiToken, nil
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	host := creds.APIHost
	if explicit.host {
		host = resolveAPIHost(cmd)
	}
	return &authInfo{source: "login", header: "Bearer " + creds.AccessToken, apiHost: host, creds: creds}, nil
}

// sawUnauthorized records whether any API response in this run was a 401,
// so reportError can suggest `fhcli login` whichever client made the call.
var sawUnauthorized atomic.Bool

type unauthorizedRecorder struct {
	next http.RoundTripper
}

func (u unauthorizedRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := u.next.RoundTrip(r)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		sawUnauthorized.Store(true)
	}
	return resp, err
}

// apiHTTPClient is shared by the SDK and rawClient.
var apiHTTPClient = &http.Client{
	Timeout:   60 * time.Second,
	Transport: unauthorizedRecorder{next: http.DefaultTransport},
}

// withDebug returns a copy of c that dumps each request and response to w,
// for --debug. The Authorization header is redacted so a pasted debug log
// never leaks a token.
func withDebug(c *http.Client, w io.Writer) *http.Client {
	next := c.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	debug := *c
	debug.Transport = debugTransport{next: next, w: w}
	return &debug
}

type debugTransport struct {
	next http.RoundTripper
	w    io.Writer
}

func (d debugTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	redacted := r.Clone(r.Context())
	if redacted.Header.Get("Authorization") != "" {
		redacted.Header.Set("Authorization", "[REDACTED]")
	}
	// DumpRequestOut consumes the body, so give it a copy and keep r's.
	if r.GetBody != nil {
		if body, err := r.GetBody(); err == nil {
			redacted.Body = body
		}
	}
	if dump, err := httputil.DumpRequestOut(redacted, r.GetBody != nil); err == nil {
		fmt.Fprintf(d.w, "--> %s\n", dump)
	}
	resp, err := d.next.RoundTrip(r)
	if err != nil {
		fmt.Fprintf(d.w, "<-- error: %v\n", err)
		return resp, err
	}
	if dump, err := httputil.DumpResponse(resp, true); err == nil {
		fmt.Fprintf(d.w, "<-- %s\n", dump)
	}
	return resp, nil
}

func withLoginHint(err error) error {
	if !sawUnauthorized.Load() {
		return err
	}
	return fmt.Errorf("%w (run `fhcli login`, or check your API token)", err)
}
