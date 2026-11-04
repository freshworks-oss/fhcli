package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/urfave/cli/v3"
)

func newUsersCmd() *cli.Command {
	return &cli.Command{
		Name:  "users",
		Usage: "look up FireHydrant users",
		Commands: []*cli.Command{
			{
				Name:      "find",
				Usage:     "find users by name or email; prints id, name and email",
				ArgsUsage: "<name or email>",
				Action:    usersFindCmd,
			},
		},
	}
}

// usersFindCmd searches users and returns only what is needed to pick one.
// An exact email (or name) match wins outright, so a similarly named user
// never needs disambiguating when the caller gave the full email.
func usersFindCmd(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return failCmd(cmd, errors.New("a name or email is required"))
	}
	term := cmd.Args().First()

	client, err := getRawClient(ctx)
	if err != nil {
		return failCmd(cmd, err)
	}
	body, err := client.get(ctx, "/v1/users", url.Values{"query": {term}, "per_page": {"20"}})
	if err != nil {
		return failCmd(cmd, err)
	}
	var resp struct {
		Data []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return failCmd(cmd, err)
	}

	type user struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	var all, exact []user
	for _, u := range resp.Data {
		all = append(all, u)
		if strings.EqualFold(u.Email, term) || strings.EqualFold(u.Name, term) {
			exact = append(exact, u)
		}
	}
	if len(exact) > 0 {
		all = exact
	}
	if all == nil {
		all = []user{}
	}
	return outputResult(cmd, map[string]any{"users": all, "exact_match": len(exact) > 0})
}
