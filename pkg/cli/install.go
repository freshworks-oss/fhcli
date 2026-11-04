package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newInstallCmd() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "install integrations for the FireHydrant CLI",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "skills",
				Usage: "output the Claude Code skill file to stdout",
			},
		},
		Action: installCmd,
	}
}

func installCmd(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("skills") {
		fmt.Print(SkillFile)
		return nil
	}

	return cli.ShowSubcommandHelp(cmd)
}
