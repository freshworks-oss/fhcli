package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultAPIHost = "https://api.firehydrant.io"

const rawClientKey ctxKey = "rawClient"

// rawClient calls the FireHydrant API directly and returns the response body
// untouched. Use it where the generated SDK's response types don't match what
// the API actually returns (the SDK silently drops unknown fields, so the
// command would print "{}").
type rawClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func getRawClient(ctx context.Context) (*rawClient, error) {
	if err, ok := ctx.Value(authErrKey).(error); ok {
		return nil, err
	}
	client, ok := ctx.Value(rawClientKey).(*rawClient)
	if !ok || client == nil {
		return nil, errNoCredentials
	}
	return client, nil
}

// get issues a GET to path (e.g. "/v1/incidents/abc/transcript").
func (c *rawClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil)
}

// do sends a request and returns the JSON response body. A non-nil body is
// sent as JSON. Non-2xx responses become errors that include the API's body,
// and an empty 2xx body (e.g. 204 No Content) is returned as {} so stdout is
// always valid JSON.
func (c *rawClient) do(ctx context.Context, method, path string, query url.Values, body []byte) (json.RawMessage, error) {
	u := strings.TrimRight(c.baseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &apiError{method: method, path: path, status: resp.StatusCode, body: strings.TrimSpace(string(respBody))}
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(respBody) {
		return nil, fmt.Errorf("%s %s: API returned non-JSON body", method, path)
	}
	return respBody, nil
}

// apiError is a non-2xx API response.
type apiError struct {
	method, path string
	status       int
	body         string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: API returned %d: %s", e.method, e.path, e.status, e.body)
}
