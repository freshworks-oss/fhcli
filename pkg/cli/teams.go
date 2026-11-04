package cli

import (
	"context"
	"errors"

	firehydrantgosdk "github.com/firehydrant/firehydrant-go-sdk"
	"github.com/firehydrant/firehydrant-go-sdk/models/operations"
	"github.com/urfave/cli/v3"
)

func newTeamsCmd() *cli.Command {
	return &cli.Command{
		Name:  "teams",
		Usage: "manage FireHydrant teams and on-call schedules",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list teams",
				Action: teamsListCmd,
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
				Usage:  "get team details including members and owned services",
				Action: teamsGetCmd,
			},
			{
				Name:   "oncall",
				Usage:  "list on-call schedules with current and next shift",
				Action: teamsOnCallCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "team-id",
						Usage: "filter by team ID",
					},
				},
			},
		},
	}
}

func teamsListCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	page := int(cmd.Int("page"))
	perPage := int(cmd.Int("per-page"))

	req := operations.ListTeamsRequest{
		Page:    firehydrantgosdk.Int(page),
		PerPage: firehydrantgosdk.Int(perPage),
	}

	if q := cmd.String("query"); q != "" {
		req.Query = firehydrantgosdk.String(q)
	}

	resp, err := client.Teams.ListTeams(ctx, req)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func teamsGetCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("team ID is required"))
	}
	teamID := cmd.Args().First()

	resp, err := client.Teams.GetTeam(ctx, teamID, nil)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}

func teamsOnCallCmd(ctx context.Context, cmd *cli.Command) error {
	client, err := getClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}

	var teamID *string
	if t := cmd.String("team-id"); t != "" {
		teamID = firehydrantgosdk.String(t)
	}

	resp, err := client.Signals.ListOrganizationOnCallSchedules(ctx, teamID)
	if err != nil {
		return failCmd(cmd, err)
	}

	return outputResult(cmd, resp)
}
