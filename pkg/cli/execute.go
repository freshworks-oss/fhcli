package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/firehydrant/firehydrant-go-sdk/models/components"
	"github.com/urfave/cli/v3"
)

func newExecuteCmd() *cli.Command {
	return &cli.Command{
		Name:   "execute",
		Usage:  "execute a command and submit metrics to the FireHydrant API",
		Action: executeCmd,
		Flags:  newSharedFlags(),
		Before: applySharedConfig,
	}
}

func executeCmd(ctx context.Context, cmd *cli.Command) error {
	// As in v0.0.15, missing credentials or a missing command fail before
	// anything runs. A stored login that can't be used (expired, unreadable)
	// still lets the command run; only the change event is skipped.
	client, clientErr := getClient(ctx)
	if errors.Is(clientErr, errNoCredentials) {
		return failCI(clientErr)
	}
	if cmd.NArg() < 1 {
		return failCI(errors.New("No command passed"))
	}

	command := cmd.Args().First()
	flags := cmd.Args().Tail()
	summary := strings.Join(cmd.Args().Slice(), " ")

	identities := parseKV(cmd.String("identities"))

	fmt.Printf("Executing command %s with args %s\n", command, strings.Join(flags, " "))

	cmdExec := exec.Command(command, flags...)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr
	start := time.Now()

	cErr := cmdExec.Run()

	duration := time.Since(start) / time.Millisecond
	now := time.Now()

	if cErr != nil {
		identities["error"] = cErr.Error()
		fmt.Printf("Executed command, duration %dms, error: %s\n", duration, cErr)
	} else {
		fmt.Printf("Executed command, duration %dms\n", duration)
	}

	if clientErr != nil {
		if err := handleError(cmd, clientErr); err != nil {
			return err
		}
		if cErr != nil {
			return handleError(cmd, cErr)
		}
		return nil
	}

	changeIdentities := make([]components.ChangeIdentity, 0, len(identities))
	for k, v := range identities {
		changeIdentities = append(changeIdentities, components.ChangeIdentity{
			Type:  k,
			Value: v,
		})
	}

	req := components.CreateChangeEvent{
		Summary:          summary,
		Environments:     paramToList(cmd.String("environment")),
		Services:         paramToList(cmd.String("service")),
		StartsAt:         &start,
		EndsAt:           &now,
		Labels:           parseKV(cmd.String("labels")),
		ChangeIdentities: changeIdentities,
	}

	resp, err := client.Changes.CreateChangeEvent(ctx, req)
	if err != nil {
		return handleError(cmd, err)
	}

	fmt.Printf("Created change event %s\n", stringValue(resp.ID))

	if cErr != nil {
		return handleError(cmd, cErr)
	}

	return nil
}
