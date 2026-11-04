package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v2"
)

type configFile struct {
	APIKey       string `yaml:"apiKey"`
	APIHost      string `yaml:"apiHost"`
	Debug        bool   `yaml:"debug"`
	IgnoreErrors bool   `yaml:"ignoreErrors"`
	Identities   string `yaml:"identities"`
	Labels       string `yaml:"labels"`
	Environment  string `yaml:"environment"`
	Service      string `yaml:"service"`
}

func newInitCmd() *cli.Command {
	return &cli.Command{
		Name:   "init",
		Usage:  "write a config file to be used in future invocations of the FireHydrant CLI",
		Action: initCmd,
		Flags:  newSharedFlags(),
	}
}

func initCmd(ctx context.Context, cmd *cli.Command) error {
	out := cmd.String("config")
	if out == "" {
		out = "/tmp/firehydrant.cfg"
	}

	config := configFile{}
	if v := cmd.String("token"); v != "" {
		config.APIKey = v
	}
	// v0.0.15 always wrote a bare host (default api.firehydrant.io), and
	// older binaries reading this file need it set.
	config.APIHost = strings.TrimPrefix(strings.TrimPrefix(cmd.String("api-host"), "https://"), "http://")
	if config.APIHost == "" {
		config.APIHost = strings.TrimPrefix(defaultAPIHost, "https://")
	}
	config.Debug = cmd.Bool("debug")
	config.IgnoreErrors = cmd.Bool("ignore-errors")

	if v := cmd.String("identities"); v != "" {
		config.Identities = v
	}
	if v := cmd.String("labels"); v != "" {
		config.Labels = v
	}
	if v := cmd.String("environment"); v != "" {
		config.Environment = v
	}
	if v := cmd.String("service"); v != "" {
		config.Service = v
	}

	d, err := yaml.Marshal(config)
	if err != nil {
		return failCI(err)
	}

	err = os.WriteFile(out, d, 0644)
	if err != nil {
		return failCI(err)
	}

	fmt.Printf("Wrote config file %s\n", out)
	return nil
}
