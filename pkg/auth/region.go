// Package auth implements `fhcli login`: an OAuth 2.0 authorization code +
// PKCE flow against FireHydrant's Doorkeeper server, credential storage, and
// token refresh. It has no CLI dependencies.
package auth

import (
	"fmt"
	"sort"
	"strings"
)

// Region is one FireHydrant instance. Each region has its own database, so
// its own OAuth application and client ID.
type Region struct {
	Name     string
	AppHost  string // serves /oauth/authorize, /oauth/token, /oauth/revoke
	APIHost  string // serves the public /v1 API
	ClientID string
}

// regions lists the known instances. Client IDs are filled in as the
// "FireHydrant CLI" OAuth application is created in each region; they are
// public-client identifiers, not secrets.
var regions = map[string]Region{
	"us":      {Name: "us", AppHost: "https://app.firehydrant.io", APIHost: "https://api.firehydrant.io", ClientID: "RrNiNdL84S0LYmRu9iVxniuLbqxF7rtno4ghwFNOcNY"},
	"eu":      subdomainRegion("eu"),
	"au":      subdomainRegion("au"),
	"in":      subdomainRegion("in"),
	"staging": {Name: "staging", AppHost: "https://app.staging.firehydrant.io", APIHost: "https://api.staging.firehydrant.io", ClientID: "TlXhAD7W8yPRUFSNwpSN4X7A4rbEzJ_9J3VV04rH9q0"},
}

func subdomainRegion(name string) Region {
	return Region{
		Name:    name,
		AppHost: "https://app." + name + ".firehydrant.io",
		APIHost: "https://api." + name + ".firehydrant.io",
	}
}

// RegionNames returns the known region names, sorted.
func RegionNames() []string {
	names := make([]string, 0, len(regions))
	for name := range regions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LookupRegion returns the named region. A non-empty clientID overrides the
// built-in one; a region with neither is an error, since login can't start.
func LookupRegion(name, clientID string) (Region, error) {
	r, ok := regions[name]
	if !ok {
		return Region{}, fmt.Errorf("unknown region %q; use one of: %s", name, strings.Join(RegionNames(), ", "))
	}
	if clientID != "" {
		r.ClientID = clientID
	}
	if r.ClientID == "" {
		return Region{}, fmt.Errorf("no OAuth client registered for region %q; pass --client-id", name)
	}
	return r, nil
}
