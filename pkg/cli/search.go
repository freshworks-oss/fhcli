package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/firehydrant/fhcli/pkg/spec"
	"github.com/urfave/cli/v3"
)

// searchHit is one `fhcli search` result: a ready-to-run command plus the
// positional args it needs.
type searchHit struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Summary string   `json:"summary"`
	// Required names the inputs the call needs besides Args: --flag for a
	// generated flag, data.<field> for a body field only settable via --data.
	Required []string `json:"required"`
	Score    float64  `json:"score"`
}

func newSearchCmd(sp *spec.Spec, loadErr error) *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "find the `fhcli api` command for a task (searches the API spec offline)",
		ArgsUsage: "<words...>",
		Description: "Ranks API operations by how well their names, summaries, paths, parameters and\n" +
			"descriptions match the query. Example: fhcli search severity matrix",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Value: 10, Usage: "maximum results (0 for all)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if loadErr != nil {
				return failCmd(cmd, loadErr)
			}
			query := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
			if query == "" {
				return failCmd(cmd, errors.New("search needs a query, e.g. `fhcli search severity matrix`"))
			}
			hits := searchHits(sp.Search(query, int(cmd.Int("limit"))))
			if cmd.Bool("interactive") {
				return printSearchTable(hits)
			}
			return outputJSON(os.Stdout, hits)
		},
	}
}

func searchHits(results []spec.Result) []searchHit {
	reserved := reservedFlagNames()
	hits := make([]searchHit, 0, len(results))
	for _, r := range results {
		op := r.Operation
		args := make([]string, 0, len(op.PathParams))
		for _, p := range op.PathParams {
			args = append(args, p.Name)
		}
		hits = append(hits, searchHit{
			Command:  apiCommandLine(op),
			Args:     args,
			Method:   op.Method,
			Path:     op.Path,
			Summary:  op.Summary,
			Required: requiredInputs(op, reserved),
			Score:    math.Round(r.Score*100) / 100,
		})
	}
	return hits
}

// requiredInputs lists op's required flags and --data-only body fields, using
// the same flag mapping as `fhcli api`, so the names match its flags.
func requiredInputs(op spec.Operation, reserved map[string]bool) []string {
	_, bindings, dataOnly := operationFlags(op, reserved)
	required := []string{}
	for _, b := range bindings {
		if b.param.Required {
			required = append(required, "--"+b.flag)
		}
	}
	for _, p := range dataOnly {
		if p.Required {
			required = append(required, "data."+p.Name)
		}
	}
	return required
}

func printSearchTable(hits []searchHit) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, h := range hits {
		command := h.Command
		for _, a := range h.Args {
			command += " <" + a + ">"
		}
		for _, r := range h.Required {
			if strings.HasPrefix(r, "--") {
				command += " " + r + " <" + strings.TrimPrefix(r, "--") + ">"
			}
		}
		fmt.Fprintf(w, "%s\t%s\n", command, h.Summary)
	}
	return w.Flush()
}
