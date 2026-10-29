package auth

import (
	"strings"
	"testing"
)

func TestLookupRegionUS(t *testing.T) {
	r, err := LookupRegion("us", "cid")
	if err != nil {
		t.Fatal(err)
	}
	if r.AppHost != "https://app.firehydrant.io" || r.APIHost != "https://api.firehydrant.io" || r.ClientID != "cid" || r.Name != "us" {
		t.Errorf("us region = %+v", r)
	}
}

func TestLookupRegionUsesSubdomainHosts(t *testing.T) {
	r, err := LookupRegion("eu", "cid")
	if err != nil {
		t.Fatal(err)
	}
	if r.AppHost != "https://app.eu.firehydrant.io" || r.APIHost != "https://api.eu.firehydrant.io" {
		t.Errorf("eu region = %+v", r)
	}
}

func TestLookupRegionUnknown(t *testing.T) {
	_, err := LookupRegion("mars", "cid")
	if err == nil || !strings.Contains(err.Error(), `unknown region "mars"`) || !strings.Contains(err.Error(), "us") {
		t.Errorf("err = %v, want unknown-region error listing the regions", err)
	}
}

func TestLookupRegionWithoutClientID(t *testing.T) {
	saved := regions["au"]
	regions["au"] = Region{Name: "au", AppHost: saved.AppHost, APIHost: saved.APIHost}
	t.Cleanup(func() { regions["au"] = saved })

	_, err := LookupRegion("au", "")
	if err == nil || !strings.Contains(err.Error(), `no OAuth client registered for region "au"; pass --client-id`) {
		t.Errorf("err = %v", err)
	}
}

func TestLookupRegionClientIDOverride(t *testing.T) {
	saved := regions["au"]
	regions["au"] = Region{Name: "au", AppHost: saved.AppHost, APIHost: saved.APIHost, ClientID: "built-in"}
	t.Cleanup(func() { regions["au"] = saved })

	r, err := LookupRegion("au", "override")
	if err != nil || r.ClientID != "override" {
		t.Errorf("r = %+v, err = %v; want the override client ID", r, err)
	}
}

func TestRegionNamesSorted(t *testing.T) {
	if got := strings.Join(RegionNames(), ","); got != "au,eu,in,staging,us" {
		t.Errorf("RegionNames() = %s", got)
	}
}

func TestLookupRegionUSHasBuiltInClientID(t *testing.T) {
	r, err := LookupRegion("us", "")
	if err != nil || r.ClientID != "RrNiNdL84S0LYmRu9iVxniuLbqxF7rtno4ghwFNOcNY" {
		t.Errorf("r = %+v, err = %v; want the US production client ID", r, err)
	}
}

func TestLookupRegionStagingHasBuiltInClientID(t *testing.T) {
	r, err := LookupRegion("staging", "")
	if err != nil || r.ClientID != "TlXhAD7W8yPRUFSNwpSN4X7A4rbEzJ_9J3VV04rH9q0" {
		t.Errorf("r = %+v, err = %v; want the staging client ID", r, err)
	}
}
