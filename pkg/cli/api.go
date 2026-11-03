package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/firehydrant/fhcli/pkg/spec"
	"github.com/urfave/cli/v3"
)

// flagBinding ties a generated CLI flag to the spec parameter it sets.
type flagBinding struct {
	flag   string     // CLI flag name, e.g. "tag-list"
	param  spec.Param // wire name and type, e.g. tag_list / array
	inBody bool       // body field (true) or query param (false)
}

// operationFunc executes a generated operation command.
type operationFunc func(ctx context.Context, cmd *cli.Command, op spec.Operation, bindings []flagBinding) error

// newAPICmd builds `fhcli api <tag> <operation>` from the spec. If the spec
// failed to load, the command reports that error instead, so a bad spec never
// breaks the other commands.
func newAPICmd(sp *spec.Spec, loadErr error, run operationFunc) *cli.Command {
	cmd := &cli.Command{
		Name:  "api",
		Usage: "call any FireHydrant API operation (generated from the OpenAPI spec)",
		Description: "Operations are grouped by API area: fhcli api <area> <operation> [args] [flags].\n" +
			"Run `fhcli api <area> --help` to list operations. Every operation supports --dry-run;\n" +
			"DELETE operations require --yes.",
	}

	if loadErr != nil {
		cmd.Action = func(ctx context.Context, c *cli.Command) error {
			return failCmd(c, loadErr)
		}
		return cmd
	}

	reserved := reservedFlagNames()
	commands := make(map[string]string, len(sp.Operations))
	for _, op := range sp.Operations {
		commands[op.ID] = apiCommandLine(op)
	}
	groups := map[string]*cli.Command{}
	for _, op := range sp.Operations {
		name := commandName(op.Tag)
		group, ok := groups[name]
		if !ok {
			group = &cli.Command{Name: name, Usage: op.Tag + " operations"}
			groups[name] = group
			cmd.Commands = append(cmd.Commands, group)
		}
		group.Commands = append(group.Commands, newOperationCmd(op, reserved, run, commands))
	}

	return cmd
}

// apiCommandLine is the `fhcli api` command that runs op.
func apiCommandLine(op spec.Operation) string {
	return "fhcli api " + commandName(op.Tag) + " " + commandName(op.ID)
}

// newOperationCmd builds the command for op. commands maps operation IDs to
// their command lines, for the "See also" section.
func newOperationCmd(op spec.Operation, reserved map[string]bool, run operationFunc, commands map[string]string) *cli.Command {
	flags, bindings, dataOnly := operationFlags(op, reserved)

	args := make([]string, len(op.PathParams))
	for i, p := range op.PathParams {
		args[i] = "<" + p.Name + ">"
	}

	var help []string
	if op.Description != "" {
		help = append(help, op.Description)
	}
	help = append(help, op.Method+" "+op.Path)
	if len(dataOnly) > 0 {
		fields := make([]string, len(dataOnly))
		for i, p := range dataOnly {
			fields[i] = describeField(p)
		}
		help = append(help, "Set these body fields with --data: "+strings.Join(fields, ", "))
	}
	if response := describeResponse(op); response != "" {
		help = append(help, response)
	}
	help = append(help, op.Notes...)
	var seeAlso []string
	for _, id := range op.SeeAlso {
		if line, ok := commands[id]; ok {
			seeAlso = append(seeAlso, line)
		}
	}
	if len(seeAlso) > 0 {
		help = append(help, "See also: "+strings.Join(seeAlso, "; "))
	}

	return &cli.Command{
		Name:        commandName(op.ID),
		Usage:       op.Summary,
		Description: strings.Join(help, "\n\n"),
		ArgsUsage:   strings.Join(args, " "),
		Flags:       flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return run(ctx, cmd, op, bindings)
		},
	}
}

// operationFlags returns the generated flags for an operation, the bindings
// that map them back to parameters, and the body fields that can only be set
// via --data (listed in help so agents can discover them). Query params win
// over body fields; names that would shadow a reserved flag are skipped.
func operationFlags(op spec.Operation, reserved map[string]bool) ([]cli.Flag, []flagBinding, []spec.Param) {
	used := map[string]bool{}
	var flags []cli.Flag
	var bindings []flagBinding
	var dataOnly []spec.Param
	listed := map[string]bool{}

	addDataOnly := func(p spec.Param) {
		if p.Name == "" || listed[p.Name] {
			return
		}
		listed[p.Name] = true
		dataOnly = append(dataOnly, p)
	}

	add := func(p spec.Param, inBody bool) {
		// Bracketed names (fields[label]) are Grape nested-hash notation:
		// meaningless as a JSON body key, so list the root key instead.
		if inBody && strings.ContainsAny(p.Name, "[]") {
			addDataOnly(spec.Param{Name: p.Name[:strings.IndexAny(p.Name, "[]")], Type: "object"})
			return
		}
		name := commandName(p.Name)
		if name == "" || reserved[name] || used[name] || !p.Scalar() {
			if inBody {
				addDataOnly(p)
			}
			return
		}
		used[name] = true
		flags = append(flags, newParamFlag(name, p, inBody))
		bindings = append(bindings, flagBinding{flag: name, param: p, inBody: inBody})
	}

	for _, p := range op.QueryParams {
		add(p, false)
	}
	for _, p := range op.BodyFields {
		add(p, true)
	}

	flags = append(flags,
		&cli.StringFlag{Name: "data", Usage: "request body as JSON: inline, @file.json, or - for stdin"},
		&cli.BoolFlag{Name: "dry-run", Usage: "print the request instead of sending it"},
		&cli.BoolFlag{Name: "yes", Usage: "confirm a destructive (DELETE) operation"},
	)

	return flags, bindings, dataOnly
}

// maxHelpFields caps how many response field names help lists before "… N fields".
const maxHelpFields = 12

// describeResponse renders the documented response shape for help, e.g.
// "Response: data[] (id, name, number), pagination"; "" when undocumented.
func describeResponse(op spec.Operation) string {
	if len(op.ResponseFields) == 0 {
		return ""
	}
	parts := make([]string, len(op.ResponseFields))
	for i, f := range op.ResponseFields {
		parts[i] = f
		if f == "data" && len(op.ResponseItemFields) > 0 {
			parts[i] = "data[] (" + listFields(op.ResponseItemFields) + ")"
		}
	}
	return "Response: " + listFields(parts)
}

func listFields(names []string) string {
	if len(names) <= maxHelpFields {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:maxHelpFields], ", ") + fmt.Sprintf(", … %d fields", len(names))
}

// describeField renders a body field for help, e.g. "impacts (array of object)".
func describeField(p spec.Param) string {
	kind := p.Type
	if p.Type == "array" && p.ItemType != "" {
		kind = "array of " + p.ItemType
	}
	if p.Required {
		kind += ", required"
	}
	return p.Name + " (" + kind + ")"
}

// newParamFlag builds the flag for p. String body flags advertise @file and
// @- (see readFlagFile); query flags take @ literally, so they don't.
func newParamFlag(name string, p spec.Param, inBody bool) cli.Flag {
	usage := p.Description
	if len(p.Enum) > 0 {
		usage += " (one of: " + strings.Join(p.Enum, ", ") + ")"
	}
	if p.Default != "" {
		usage += " (server default: " + p.Default + ")"
	}
	if p.Required {
		usage += " (required)"
	}
	if inBody && isTextParam(p) {
		usage += " (accepts @file, or @- for stdin)"
	}
	// urfave/cli treats `text` in a usage string as the value placeholder.
	usage = strings.TrimSpace(strings.ReplaceAll(usage, "`", "'"))

	switch p.Type {
	case "integer":
		return &cli.IntFlag{Name: name, Usage: usage}
	case "number":
		return &cli.FloatFlag{Name: name, Usage: usage}
	case "boolean":
		return &cli.BoolFlag{Name: name, Usage: usage}
	case "array":
		return &cli.StringSliceFlag{Name: name, Usage: usage}
	default:
		return &cli.StringFlag{Name: name, Usage: usage}
	}
}

// isTextParam reports whether p becomes a plain string flag.
func isTextParam(p spec.Param) bool {
	switch p.Type {
	case "integer", "number", "boolean", "array":
		return false
	}
	return true
}

// reservedFlagNames are names generated flags may not use: root flags are
// persistent in urfave/cli v3 (they apply to every subcommand), and data,
// dry-run, and yes are added to every generated command.
func reservedFlagNames() map[string]bool {
	reserved := map[string]bool{
		"help": true, "h": true, "version": true, "v": true,
		"data": true, "dry-run": true, "yes": true,
	}
	for _, f := range newGlobalFlags() {
		for _, n := range f.Names() {
			reserved[n] = true
		}
	}
	return reserved
}

// commandName turns a tag, operationId, or param name into a kebab-case CLI
// name: "Incident Settings" → "incident-settings", "list_incidents" →
// "list-incidents", "startIndex" → "start-index", "SCIM" → "scim".
func commandName(s string) string {
	var b strings.Builder
	pendingDash := false
	prevLowerOrDigit := false

	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			if prevLowerOrDigit {
				pendingDash = true
			}
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			pendingDash, prevLowerOrDigit = false, false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			pendingDash, prevLowerOrDigit = false, true
		default:
			pendingDash, prevLowerOrDigit = true, false
		}
	}
	return b.String()
}

// stdinReader is where `--data -` reads from; tests replace it.
var stdinReader io.Reader = os.Stdin

// apiRequest is a fully resolved request for one operation.
type apiRequest struct {
	Method string
	Path   string // path with args substituted, e.g. /v1/incidents/abc
	Query  url.Values
	Body   map[string]any // nil when no body is sent
}

type dryRunOutput struct {
	Method string         `json:"method"`
	URL    string         `json:"url"`
	Query  url.Values     `json:"query,omitempty"`
	Body   map[string]any `json:"body,omitempty"`
	Note   string         `json:"note,omitempty"`
}

// runOperation builds the request for op and either prints it (--dry-run)
// or sends it. DELETE operations require --yes.
func runOperation(ctx context.Context, cmd *cli.Command, op spec.Operation, bindings []flagBinding) error {
	args := cmd.Args().Slice()
	req, err := buildRequest(cmd, op, bindings, args)
	if err != nil {
		return failCmd(cmd, err)
	}
	numberAt := incidentNumberArg(op, args)
	relatedNumber, relatedIsNumber := relatedToIncidentNumber(req.Body)

	if cmd.Bool("dry-run") {
		out := dryRunOutput{
			Method: req.Method,
			URL:    requestHost(ctx, cmd) + req.Path,
			Query:  req.Query,
			Body:   req.Body,
		}
		var notes []string
		if numberAt >= 0 {
			number, _ := incidentNumber(args[numberAt])
			notes = append(notes, fmt.Sprintf("incident number %s is resolved to its ID when sent", number))
		}
		if relatedIsNumber {
			notes = append(notes, fmt.Sprintf("related_to incident number %s is resolved to its ID when sent", relatedNumber))
		}
		out.Note = strings.Join(notes, "; ")
		return outputJSON(os.Stdout, out)
	}

	if req.Method == http.MethodDelete && !cmd.Bool("yes") {
		return failCmd(cmd, fmt.Errorf("%s is destructive; re-run with --yes", op.ID))
	}

	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if numberAt >= 0 {
		number, _ := incidentNumber(args[numberAt])
		id, err := resolveIncidentNumber(ctx, client, number)
		if err != nil {
			return failCmd(cmd, err)
		}
		args[numberAt] = id
		req.Path = fillPath(op, args)
	}
	if relatedIsNumber {
		id, err := resolveIncidentNumber(ctx, client, relatedNumber)
		if err != nil {
			return failCmd(cmd, err)
		}
		req.Body["related_to"] = "incident/" + id
	}

	var body []byte
	if req.Body != nil {
		if body, err = json.Marshal(req.Body); err != nil {
			return failCmd(cmd, err)
		}
	}

	resp, err := client.do(ctx, req.Method, req.Path, req.Query, body)
	if err != nil {
		return failCmd(cmd, withTimeoutHint(op, err))
	}
	return outputResult(cmd, resp)
}

// withTimeoutHint adds advice to a timed-out request (a 504/408 from the API
// or a client-side timeout): narrow it with the operation's own date/time
// filter flags when it has any, since slow list endpoints are usually
// scanning too wide a window.
func withTimeoutHint(op spec.Operation, err error) error {
	var ae *apiError
	var ne net.Error
	timedOut := (errors.As(err, &ae) && (ae.status == http.StatusGatewayTimeout || ae.status == http.StatusRequestTimeout)) ||
		(errors.As(err, &ne) && ne.Timeout()) || errors.Is(err, context.DeadlineExceeded)
	if !timedOut {
		return err
	}
	var filters []string
	paginated := false
	for _, p := range op.QueryParams {
		if strings.Contains(p.Name, "date") || strings.Contains(p.Name, "time") || strings.HasPrefix(p.Name, "since") {
			filters = append(filters, "--"+commandName(p.Name))
		}
		paginated = paginated || p.Name == "per_page"
	}
	switch {
	case len(filters) > 0:
		return fmt.Errorf("%w (the request timed out; narrow it with %s)", err, strings.Join(filters, ", "))
	case paginated:
		return fmt.Errorf("%w (the request timed out; try narrower filters or a smaller --per-page, then retry)", err)
	}
	return fmt.Errorf("%w (the request timed out; retry, and check status.firehydrant.com if it persists)", err)
}

// buildRequest substitutes path args, collects set query flags, and merges
// --data with set body flags (flags win).
func buildRequest(cmd *cli.Command, op spec.Operation, bindings []flagBinding, args []string) (*apiRequest, error) {
	if len(args) < len(op.PathParams) {
		return nil, fmt.Errorf("%s is required", op.PathParams[len(args)].Name)
	}
	if len(args) > len(op.PathParams) {
		return nil, fmt.Errorf("unexpected argument %q", args[len(op.PathParams)])
	}

	req := &apiRequest{Method: op.Method, Path: fillPath(op, args), Query: url.Values{}}

	if cmd.String("data") == "-" {
		for _, b := range bindings {
			if b.inBody && cmd.IsSet(b.flag) && cmd.String(b.flag) == "@-" {
				return nil, fmt.Errorf("--data - and --%s @- both read stdin, which can only be read once", b.flag)
			}
		}
	}

	body, err := readData(cmd.String("data"))
	if err != nil {
		return nil, err
	}
	if body != nil && !op.HasBody {
		return nil, fmt.Errorf("%s takes no request body; remove --data", op.ID)
	}

	stdinUsed := cmd.String("data") == "-"
	for _, b := range bindings {
		if !cmd.IsSet(b.flag) {
			continue
		}
		value := flagValue(cmd, b)
		if text, ok := value.(string); ok && b.inBody && strings.HasPrefix(text, "@") {
			if value, err = readFlagFile(b.flag, text, &stdinUsed); err != nil {
				return nil, err
			}
		}
		if b.inBody {
			if body == nil {
				body = map[string]any{}
			}
			body[b.param.Name] = value
			continue
		}
		if values, ok := value.([]string); ok {
			key := arrayQueryKey(b.param.Name)
			for _, v := range values {
				req.Query.Add(key, v)
			}
		} else {
			req.Query.Set(b.param.Name, fmt.Sprint(value))
		}
	}

	req.Body = body
	return req, nil
}

// fillPath substitutes positional args into the operation's path template.
func fillPath(op spec.Operation, args []string) string {
	path := op.Path
	for i, p := range op.PathParams {
		path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(args[i]))
	}
	return path
}

// incidentNumberArg returns the index of an incident_id arg given as an
// incident number ("623" or "#623"), or -1. Some endpoints accept numbers and
// some silently return nothing for them, so the CLI resolves numbers itself.
func incidentNumberArg(op spec.Operation, args []string) int {
	for i, p := range op.PathParams {
		if p.Name != "incident_id" || i >= len(args) {
			continue
		}
		if _, ok := incidentNumber(args[i]); ok {
			return i
		}
	}
	return -1
}

// incidentNumber parses an incident number written as "623" or "#623".
func incidentNumber(s string) (string, bool) {
	s = strings.TrimPrefix(s, "#")
	return s, isAllDigits(s)
}

// relatedToIncidentNumber reports a body related_to of the form
// "incident/<number>" (e.g. on create-ticket), which the API only accepts
// with the incident's ID.
func relatedToIncidentNumber(body map[string]any) (string, bool) {
	ref, ok := body["related_to"].(string)
	if !ok || !strings.HasPrefix(ref, "incident/") {
		return "", false
	}
	return incidentNumber(strings.TrimPrefix(ref, "incident/"))
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolveIncidentNumber looks up an incident by number and returns its ID.
func resolveIncidentNumber(ctx context.Context, client *rawClient, number string) (string, error) {
	body, err := client.get(ctx, "/v1/incidents/"+number, nil)
	if err != nil {
		return "", fmt.Errorf("resolve incident number %s: %w", number, err)
	}
	var incident struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &incident); err != nil || incident.ID == "" {
		return "", fmt.Errorf("resolve incident number %s: response has no incident id", number)
	}
	return incident.ID, nil
}

// arrayQueryKey returns the Rack array form of a query param name, which is
// what Grape parses as an Array: "include" → "include[]",
// "custom_fields[field_id]" → "custom_fields[][field_id]". A repeated bare
// key keeps only the last value, and parent[child] parses as a Hash.
func arrayQueryKey(name string) string {
	if i := strings.Index(name, "["); i >= 0 {
		return name[:i] + "[]" + name[i:]
	}
	return name + "[]"
}

func flagValue(cmd *cli.Command, b flagBinding) any {
	switch b.param.Type {
	case "integer":
		return cmd.Int(b.flag)
	case "number":
		return cmd.Float(b.flag)
	case "boolean":
		return cmd.Bool(b.flag)
	case "array":
		return cmd.StringSlice(b.flag)
	default:
		return cmd.String(b.flag)
	}
}

// readFlagFile reads a string body flag given as @path or @- (stdin), like
// --data. A missing file is an error rather than a literal "@…" value, so a
// typo never sends the file name as text; values that really start with @
// go through --data.
func readFlagFile(flag, value string, stdinUsed *bool) (string, error) {
	var raw []byte
	var err error
	if value == "@-" {
		if *stdinUsed {
			return "", fmt.Errorf("--%s @-: stdin can only be read once (already used by another flag)", flag)
		}
		*stdinUsed = true
		raw, err = io.ReadAll(stdinReader)
	} else {
		raw, err = os.ReadFile(strings.TrimPrefix(value, "@"))
	}
	if err != nil {
		return "", fmt.Errorf("--%s %s: %w (to send text starting with @, set it with --data)", flag, value, err)
	}
	return string(raw), nil
}

// readData parses --data: inline JSON, @path to a file, or - for stdin. The
// value must be a JSON object; "" means no --data.
func readData(value string) (map[string]any, error) {
	if value == "" {
		return nil, nil
	}

	var raw []byte
	var err error
	switch {
	case value == "-":
		raw, err = io.ReadAll(stdinReader)
	case strings.HasPrefix(value, "@"):
		raw, err = os.ReadFile(strings.TrimPrefix(value, "@"))
	default:
		raw = []byte(value)
	}
	if err != nil {
		return nil, fmt.Errorf("read --data: %w", err)
	}

	// UseNumber keeps numbers as written: decoding to float64 would round
	// integers above 2^53 (IDs) and re-encode large ones as 1e+21.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil || body == nil {
		return nil, errors.New("--data must be a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("--data must be a single JSON object")
	}
	return body, nil
}

// requestHost is the API host a request is sent to: the resolved auth's host
// (a login uses its region's), else --api-host.
func requestHost(ctx context.Context, cmd *cli.Command) string {
	if info, ok := ctx.Value(authInfoKey).(*authInfo); ok && info != nil {
		return info.apiHost
	}
	return resolveAPIHost(cmd)
}
