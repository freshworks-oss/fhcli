package cli

import (
	"context"
	"errors"

	firehydrantgosdk "github.com/firehydrant/firehydrant-go-sdk"
	"github.com/firehydrant/firehydrant-go-sdk/models/operations"
	"github.com/urfave/cli/v3"
)

func newServicesCmd() *cli.Command {
	return &cli.Command{
		Name:  "services",
		Usage: "manage FireHydrant service catalog",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list services",
				Action: servicesListCmd,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "page",
						Usage: "page number",
						Value: 1,
					},
					&cli.IntFlag{
						Name:  "per-page",
						Usage: "results per page",
						Value: 20,
					},
					&cli.StringFlag{
						Name:    "query",
						Aliases: []string{"q"},
						Usage:   "search by name or description",
					},
				},
			},
			{
				Name:   "get",
				Usage:  "get service details including links, owner, and environments",
				Action: servicesGetCmd,
			},
		},
	}
}

func servicesListCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	page := int(cmd.Int("page"))
	perPage := int(cmd.Int("per-page"))

	req := operations.ListServicesRequest{
		Page:    firehydrantgosdk.Int(page),
		PerPage: firehydrantgosdk.Int(perPage),
	}

	if q := cmd.String("query"); q != "" {
		req.Query = firehydrantgosdk.String(q)
	}

	resp, err := client.CatalogEntries.ListServices(ctx, req)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func servicesGetCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("service ID is required"))
	}
	serviceID := cmd.Args().First()

	resp, err := client.CatalogEntries.GetService(ctx, serviceID)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}
