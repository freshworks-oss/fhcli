package main

import (
	"context"
	"os"
	"runtime/debug"

	"github.com/firehydrant/fhcli/pkg/cli"
)

// Set with -ldflags by goreleaser and `make build`.
var commit string
var version string

func main() {
	cmd := cli.NewRootCmd(buildCommit(), buildVersion())
	// Commands report their own errors (JSON on stderr) and urfave/cli prints
	// usage errors, so just set the exit code here.
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}

// buildVersion falls back to the module version for `go install` builds and
// to "dev" for a plain `go build`.
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// buildCommit falls back to the VCS revision Go stamps into the binary.
func buildCommit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "unknown"
}
