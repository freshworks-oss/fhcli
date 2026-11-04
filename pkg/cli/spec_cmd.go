package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/firehydrant/fhcli/pkg/auth"
	"github.com/firehydrant/fhcli/pkg/spec"
	"github.com/urfave/cli/v3"
)

const defaultSpecURL = "https://api.firehydrant.io/v1/swagger_doc"

func newSpecCmd() *cli.Command {
	return &cli.Command{
		Name:  "spec",
		Usage: "manage the API spec that powers `fhcli api`",
		Commands: []*cli.Command{
			{
				Name:  "update",
				Usage: "download the latest API spec",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Value: defaultSpecURL, Usage: "spec URL"},
				},
				Action: specUpdateCmd,
			},
			{
				Name:   "info",
				Usage:  "show which API spec is in use",
				Action: specInfoCmd,
			},
		},
	}
}

func specUpdateCmd(ctx context.Context, cmd *cli.Command) error {
	specURL := cmd.String("url")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return failCmd(cmd, err)
	}
	resp, err := apiHTTPClient.Do(req)
	if err != nil {
		return failCmd(cmd, fmt.Errorf("download spec: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return failCmd(cmd, fmt.Errorf("download spec: %s returned %d", specURL, resp.StatusCode))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return failCmd(cmd, fmt.Errorf("download spec: %w", err))
	}

	sp, err := spec.Parse(data)
	if err != nil {
		return failCmd(cmd, fmt.Errorf("downloaded spec is invalid; keeping the current one: %w", err))
	}

	path, err := spec.OverridePath()
	if err != nil {
		return failCmd(cmd, err)
	}
	if err := auth.WriteFileAtomic(path, data); err != nil {
		return failCmd(cmd, fmt.Errorf("save spec: %w", err))
	}

	return outputJSON(os.Stdout, map[string]any{
		"source":     path,
		"operations": len(sp.Operations),
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func specInfoCmd(ctx context.Context, cmd *cli.Command) error {
	sp, err := spec.Load()
	if err != nil {
		return failCmd(cmd, err)
	}

	tags := map[string]bool{}
	for _, op := range sp.Operations {
		tags[op.Tag] = true
	}

	info := map[string]any{
		"source":     sp.Source,
		"operations": len(sp.Operations),
		"tags":       len(tags),
	}
	if !sp.ModifiedAt.IsZero() {
		info["modified_at"] = sp.ModifiedAt.UTC().Format(time.RFC3339)
	}
	return outputJSON(os.Stdout, info)
}
