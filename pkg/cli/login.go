package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/firehydrant/fhcli/pkg/auth"
	"github.com/urfave/cli/v3"
)

// Test seams.
var (
	lookupRegion = auth.LookupRegion
	loginPorts   []int         // nil means auth.DefaultPorts
	loginTimeout time.Duration // zero means the auth default
	openBrowser  = openInBrowser
)

var regionFlag = &cli.StringFlag{
	Name:  "region",
	Usage: "FireHydrant region: " + strings.Join(auth.RegionNames(), ", ") + " (default: the region you last logged in to, else us)",
}

func newLoginCmd() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "log in to FireHydrant in your browser and store the token",
		Flags: []cli.Flag{
			regionFlag,
			&cli.StringFlag{Name: "org", Usage: "organization ID to preselect on the consent screen"},
			&cli.StringFlag{Name: "client-id", Usage: "OAuth client ID, overriding the built-in one for the region", Sources: cli.EnvVars("FH_OAUTH_CLIENT_ID")},
			&cli.BoolFlag{Name: "no-browser", Usage: "print the login URL instead of opening a browser"},
		},
		Action: loginCmd,
	}
}

func newLogoutCmd() *cli.Command {
	return &cli.Command{
		Name:   "logout",
		Usage:  "revoke and delete the stored login",
		Flags:  []cli.Flag{regionFlag},
		Action: logoutCmd,
	}
}

func newWhoamiCmd() *cli.Command {
	return &cli.Command{
		Name:   "whoami",
		Usage:  "show who the CLI is authenticated as, in which organization and region",
		Action: whoamiCmd,
	}
}

func selectedRegion(cmd *cli.Command) string {
	if r := cmd.String("region"); r != "" {
		return r
	}
	return auth.CurrentRegion()
}

func loginCmd(ctx context.Context, cmd *cli.Command) error {
	region, err := lookupRegion(selectedRegion(cmd), cmd.String("client-id"))
	if err != nil {
		return failCmd(cmd, err)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	opts := auth.LoginOptions{
		Ports:          loginPorts,
		OrganizationID: cmd.String("org"),
		Prompt:         os.Stderr,
		Timeout:        loginTimeout,
	}
	if !cmd.Bool("no-browser") {
		opts.OpenBrowser = openBrowser
	}
	creds, err := auth.Login(ctx, region, opts)
	if err != nil {
		return failCmd(cmd, err)
	}
	if err := auth.DefaultStore().Save(creds); err != nil {
		return failCmd(cmd, err)
	}
	if err := auth.SetCurrentRegion(region.Name); err != nil {
		return failCmd(cmd, err)
	}
	return outputResult(cmd, map[string]string{
		"user":            creds.User,
		"email":           creds.Email,
		"organization":    creds.Organization,
		"organization_id": creds.OrganizationID,
		"region":          creds.Region,
	})
}

func logoutCmd(ctx context.Context, cmd *cli.Command) error {
	region := selectedRegion(cmd)
	store := auth.DefaultStore()
	// An unreadable login can't be revoked, but it is still deleted.
	if creds, err := store.Load(region); err == nil {
		auth.Revoke(ctx, creds)
	}
	if err := store.Delete(region); err != nil {
		return failCmd(cmd, err)
	}
	return outputResult(cmd, map[string]string{"logged_out": region})
}

func whoamiCmd(ctx context.Context, cmd *cli.Command) error {
	info, _ := ctx.Value(authInfoKey).(*authInfo)
	if info == nil {
		if err, ok := ctx.Value(authErrKey).(error); ok {
			return failCmd(cmd, err)
		}
		return failCmd(cmd, errNoCredentials)
	}
	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}
	body, err := client.get(ctx, "/v1/current_user", nil)
	if err != nil {
		return failCmd(cmd, err)
	}
	var user auth.Identity
	if err := json.Unmarshal(body, &user); err != nil {
		return failCmd(cmd, errors.New("unexpected /v1/current_user response"))
	}
	out := map[string]string{
		"source":          info.source,
		"api_host":        info.apiHost,
		"user":            user.Name,
		"email":           user.Email,
		"organization":    user.OrganizationName,
		"organization_id": user.OrganizationID,
	}
	if info.creds != nil {
		out["region"] = info.creds.Region
		if !info.creds.Expiry.IsZero() {
			out["expires_at"] = info.creds.Expiry.UTC().Format(time.RFC3339)
		}
	}
	return outputResult(cmd, out)
}

func openInBrowser(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}
