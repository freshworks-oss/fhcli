package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/firehydrant/fhcli/pkg/spec"
	firehydrantgosdk "github.com/firehydrant/firehydrant-go-sdk"
	"github.com/firehydrant/firehydrant-go-sdk/models/components"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v2"
)

type ctxKey string

const sdkClientKey ctxKey = "sdkClient"

// newSharedFlags returns fresh change-event flags. Each command needs its
// own: urfave/cli keeps parsed state on flag values, so sharing them across
// command trees (as tests do) would make a flag look set when it isn't.
func newSharedFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "identities",
			Usage:   "identities for the event, comma separated. example: image=quay.io/firehydrant/rails:abcdef0",
			Sources: cli.EnvVars("FH_IDENTITIES"),
		},
		&cli.StringFlag{
			Name:    "labels",
			Usage:   "labels for the event, comma separated. example: executor=jenkins",
			Sources: cli.EnvVars("FH_LABELS"),
		},
		&cli.StringFlag{
			Name:    "environment",
			Usage:   "environment for the event, example: production",
			Sources: cli.EnvVars("FH_ENVIRONMENT"),
		},
		&cli.StringFlag{
			Name:    "service",
			Usage:   "service for the event, example: rails-monolith",
			Sources: cli.EnvVars("FH_SERVICE"),
		},
	}
}

// newGlobalFlags returns fresh root flags. Each root command needs its own:
// urfave/cli keeps parsed state on flag values, so sharing them across roots
// (as tests do) would stop env var sources from being re-read.
func newGlobalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "token",
			Aliases: []string{"apiKey"},
			Usage:   "FireHydrant API token",
			Sources: cli.EnvVars("FIREHYDRANT_API_TOKEN", "FH_API_KEY"),
		},
		&cli.StringFlag{
			Name:    "api-host",
			Aliases: []string{"apiHost"},
			Usage:   "FireHydrant API hostname",
			Value:   defaultAPIHost,
			Sources: cli.EnvVars("FH_API_HOST"),
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug output for API calls",
		},
		&cli.BoolFlag{
			Name:    "interactive",
			Aliases: []string{"i"},
			Usage:   "enable human-readable output instead of JSON",
		},
		&cli.BoolFlag{
			Name:    "ignore-errors",
			Aliases: []string{"ignoreErrors"},
			Usage:   "exit 0 on errors from FH API",
			Value:   true,
		},
		&cli.StringFlag{
			Name:    "config",
			Usage:   "config file path",
			Sources: cli.EnvVars("FH_CONFIG_FILE"),
		},
	}
}

// NewRootCmd builds the CLI with the API spec from spec.Load.
func NewRootCmd(commit, version string) *cli.Command {
	sp, err := spec.Load()
	return newRootCmd(commit, version, sp, err)
}

func newRootCmd(commit, version string, sp *spec.Spec, specErr error) *cli.Command {
	cmd := &cli.Command{
		Name:    "fhcli",
		Usage:   "FireHydrant CLI",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		Flags:   newGlobalFlags(),
		Before:  rootBefore,
		Commands: []*cli.Command{
			newInitCmd(),
			newEventCmd(),
			newExecuteCmd(),
			newIncidentsCmd(),
			newServicesCmd(),
			newTeamsCmd(),
			newAPICmd(sp, specErr, runOperation),
			newSearchCmd(sp, specErr),
			newSpecCmd(),
			newLoginCmd(),
			newLogoutCmd(),
			newWhoamiCmd(),
			newInstallCmd(),
		},
	}

	return cmd
}

func rootBefore(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	sawUnauthorized.Store(false)
	cmdName := ""
	if cmd.Args().Present() {
		cmdName = cmd.Args().First()
	}
	// init writes a config file from its flags alone, as in v0.0.15: an
	// existing (possibly another org's, or planted /tmp) config must not
	// leak its apiKey or apiHost into the new file.
	if cmdName == "init" {
		return ctx, nil
	}

	// Decide before the config file is applied: a config file (which may be
	// in /tmp) must not override a stored login, nor redirect its token.
	explicit := explicitAuth{token: cmd.IsSet("token"), host: cmd.IsSet("api-host")}
	loadConfigFile(cmd)

	// Skip auth for commands that don't call the API with it
	switch cmdName {
	case "install", "spec", "search", "login", "logout":
		return ctx, nil
	}

	info, err := resolveAuth(ctx, cmd, explicit)
	if err != nil {
		// Reported by the command via getClient, so --ignore-errors still
		// applies to event/execute.
		return context.WithValue(ctx, authErrKey, err), nil
	}
	if info == nil {
		return ctx, nil
	}

	httpClient := apiHTTPClient
	if cmd.Bool("debug") {
		httpClient = withDebug(apiHTTPClient, os.Stderr)
	}
	opts := []firehydrantgosdk.SDKOption{
		firehydrantgosdk.WithSecurity(components.Security{APIKey: info.header}),
		firehydrantgosdk.WithClient(httpClient),
	}
	if info.apiHost != defaultAPIHost {
		opts = append(opts, firehydrantgosdk.WithServerURL(info.apiHost))
	}

	ctx = context.WithValue(ctx, authInfoKey, info)
	ctx = context.WithValue(ctx, sdkClientKey, firehydrantgosdk.New(opts...))
	ctx = context.WithValue(ctx, rawClientKey, &rawClient{baseURL: info.apiHost, token: info.header, http: httpClient})
	return ctx, nil
}

// resolveAPIHost returns --api-host with a scheme and no trailing slash,
// defaulting to production.
func resolveAPIHost(cmd *cli.Command) string {
	host := strings.TrimRight(cmd.String("api-host"), "/")
	if host == "" {
		return defaultAPIHost
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	return host
}

func getClient(ctx context.Context) (*firehydrantgosdk.FireHydrant, error) {
	if err, ok := ctx.Value(authErrKey).(error); ok {
		return nil, err
	}
	client, ok := ctx.Value(sdkClientKey).(*firehydrantgosdk.FireHydrant)
	if !ok || client == nil {
		return nil, errNoCredentials
	}
	return client, nil
}

// Config file keys (as written by `fhcli init`) and the flags they set.
// Root flags are applied once by rootBefore; the shared change-event flags
// belong to the event and execute subcommands, so each applies them in its
// own Before (see applySharedConfig).
var (
	rootConfigKeys = map[string]string{
		"apiKey":       "token",
		"apiHost":      "api-host",
		"debug":        "debug",
		"ignoreErrors": "ignore-errors",
	}
	sharedConfigKeys = map[string]string{
		"identities":  "identities",
		"labels":      "labels",
		"environment": "environment",
		"service":     "service",
	}
)

// readConfigFile returns the first config file found: --config, then
// /etc/firehydrant.cfg, ~/firehydrant.cfg, /tmp/firehydrant.cfg. A missing
// or unreadable file is no config.
func readConfigFile(cmd *cli.Command) map[string]any {
	paths := []string{
		cmd.String("config"),
		path.Join("/etc", "firehydrant.cfg"),
		path.Join(os.Getenv("HOME"), "firehydrant.cfg"),
		path.Join("/tmp", "firehydrant.cfg"),
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg map[string]any
		if yaml.Unmarshal(data, &cfg) != nil {
			return nil
		}
		return cfg
	}
	return nil
}

// applyConfig sets each flag in keys from cfg unless the flag was already set
// on the command line or by an env var (both take precedence, as in v0.0.15).
// Empty strings are skipped; booleans apply whether true or false.
func applyConfig(cmd *cli.Command, cfg map[string]any, keys map[string]string) {
	for key, flag := range keys {
		val, ok := cfg[key]
		if !ok || val == nil || cmd.IsSet(flag) {
			continue
		}
		switch v := val.(type) {
		case string:
			if v != "" {
				_ = cmd.Set(flag, v)
			}
		case bool:
			_ = cmd.Set(flag, strconv.FormatBool(v))
		}
	}
}

// loadConfigFile applies the config file's root flag values.
func loadConfigFile(cmd *cli.Command) {
	applyConfig(cmd, readConfigFile(cmd), rootConfigKeys)
}

// applySharedConfig is the Before hook of event and execute: it applies the
// config file's identities, labels, environment and service to their flags.
func applySharedConfig(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	applyConfig(cmd, readConfigFile(cmd), sharedConfigKeys)
	return ctx, nil
}

// outputJSON writes data as JSON: indented when w is a terminal, one compact
// line otherwise, so agents and pipes get terse output that costs fewer tokens.
func outputJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	if isTerminal(w) {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(data)
}

// isTerminal reports whether w is a character device (a terminal), as
// opposed to a pipe, file, or buffer.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// outputResult writes data as JSON to stdout, or human-readable if --interactive is set.
func outputResult(cmd *cli.Command, data any) error {
	if cmd.Bool("interactive") {
		return outputInteractive(os.Stdout, data)
	}
	return outputJSON(os.Stdout, data)
}

// outputInteractive writes a simple human-readable representation.
func outputInteractive(w io.Writer, data any) error {
	// Marshal to JSON then unmarshal to a generic structure for pretty printing
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// handleError reports an error the way v0.0.15 did and respects
// --ignore-errors. It is for the change-event commands (event, execute,
// init), which CI pipelines run: the message goes to stdout, and by default
// the exit code is 0 so a FireHydrant outage never fails a pipeline.
func handleError(cmd *cli.Command, err error) error {
	fmt.Println(withLoginHint(err).Error())
	if cmd.Bool("ignore-errors") {
		fmt.Println("ignoreErrors is set so exit code will be 0")
		return nil
	}
	fmt.Fprintln(os.Stderr, withLoginHint(err).Error())
	return err
}

// failCI fails a change-event command outright, before doing anything, the
// way v0.0.15 did for configuration errors (no API key, no command): the
// message goes to stderr and the exit code is non-zero regardless of
// --ignore-errors, so a misconfigured pipeline is noticed.
func failCI(err error) error {
	fmt.Fprintln(os.Stderr, err.Error())
	return err
}

// failCmd reports an error and always returns it so the CLI exits non-zero.
// Incident, service, and team commands use it: agents and scripts rely on the
// exit code to tell a failed lookup or write from a successful one.
func failCmd(cmd *cli.Command, err error) error {
	reportError(cmd, err)
	return err
}

func reportError(cmd *cli.Command, err error) {
	err = withLoginHint(err)
	if cmd.Bool("interactive") {
		fmt.Fprintln(os.Stderr, "Error:", err.Error())
	} else {
		outputJSON(os.Stderr, errorOutput(err))
	}
}

// errorOutput is the JSON written to stderr for err. "error" is always the
// message. An API failure adds status, method, path and the response body
// (parsed when it is JSON), so agents can branch on .status and read
// .response.detail without unescaping the message.
func errorOutput(err error) map[string]any {
	out := map[string]any{"error": err.Error()}
	var ae *apiError
	if !errors.As(err, &ae) {
		return out
	}
	out["status"] = ae.status
	out["method"] = ae.method
	out["path"] = ae.path
	var body any
	switch {
	case ae.body == "":
	case json.Unmarshal([]byte(ae.body), &body) == nil:
		out["response"] = body
	default:
		out["response"] = ae.body
	}
	return out
}

// parseKV parses a "k=v,k2=v2" string into a map.
func parseKV(s string) map[string]string {
	result := make(map[string]string)
	if s == "" {
		return result
	}
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return result
}

// paramToList splits a comma-separated string into a list of trimmed strings.
func paramToList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
