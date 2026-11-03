# FireHydrant CLI (fhcli)

![GitHub release (latest by date)](https://img.shields.io/github/v/release/freshworks-oss/fhcli?display_name=release)

`fhcli` is the command line for [FireHydrant](https://firehydrant.com). Use it to:

- **Record change events from CI and infrastructure.** Deploys, image builds, Terraform runs and cron jobs show up on incidents as likely causes.
- **Work incidents from the terminal.** Triage, post notes, check who's on call, and resolve.
- **Call any public API operation.** All 479 operations are generated from the OpenAPI spec and can be searched offline.
- **Give an AI agent FireHydrant access.** Output is compact JSON when piped, errors are structured, and a Claude Code skill is included.

- [Installation](#installation)
- [Authentication](#authentication)
- [Change events (CI and infrastructure)](#change-events-ci-and-infrastructure)
- [Incidents, services and teams](#incidents-services-and-teams)
- [Any API operation: `fhcli api`](#any-api-operation-fhcli-api)
- [Using fhcli from an AI agent](#using-fhcli-from-an-ai-agent)
- [Output and exit codes](#output-and-exit-codes)
- [Configuration reference](#configuration-reference)
- [Development](#development)

## Installation

Every release on the [releases page](https://github.com/freshworks-oss/fhcli/releases) has binaries for macOS (`darwin_amd64`, `darwin_arm64`) and Linux (`linux_amd64`, `linux_arm64`, `linux_386`). The assets are named `fhcli_{version}_{os}_{arch}.tar.gz` and are listed in `checksums.txt`.

```sh
VERSION=0.1.0                                   # see the releases page for the latest
OS=$(uname -s | tr '[:upper:]' '[:lower:]')     # darwin or linux
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -fsSL "https://github.com/freshworks-oss/fhcli/releases/download/v${VERSION}/fhcli_${VERSION}_${OS}_${ARCH}.tar.gz" \
  | tar -xz -C /usr/local/bin fhcli
fhcli --version
```

For CI, pin `VERSION` so builds are reproducible. On macOS, a binary downloaded through a browser (rather than with `curl`) is quarantined by Gatekeeper; clear the flag with `xattr -d com.apple.quarantine /usr/local/bin/fhcli`.

To build from source with Go 1.24+, run `git clone https://github.com/freshworks-oss/fhcli && cd fhcli && make install`. This installs to `$(go env GOPATH)/bin`.

## Authentication

There are two ways to authenticate. Use whichever fits where the CLI runs.

**Interactive use: log in with your browser.**

```sh
fhcli login        # opens your browser; pick the organization and approve
fhcli whoami       # who and where the CLI is acting
fhcli logout       # revoke and delete the stored login
```

The login is stored in the macOS Keychain. On Linux, or with `FHCLI_TOKEN_STORE=file`, it's stored in a `0600` file under `~/.config/fhcli/credentials/`. It refreshes automatically. `fhcli login` listens on `localhost` ports 8085–8087 for the browser redirect; `--no-browser` prints the URL instead of opening it.

Regions: `fhcli login --region us|eu|au|in|staging`. Later commands use the region you last logged in to. Built-in OAuth clients exist for `us` and `staging`. In the other regions, pass `--client-id` (or set `FH_OAUTH_CLIENT_ID`) until their clients ship.

**CI, bots and scripts: use an API token.**

```sh
export FIREHYDRANT_API_TOKEN=fhb-...   # FH_API_KEY also works
```

A token from `--token`/`--apiKey`, the environment, or a config file **takes precedence over a stored login**. If `fhcli whoami` shows a bot instead of you, an exported token is shadowing your login. Some endpoints are only available to bot tokens.

## Change events (CI and infrastructure)

When an incident opens, FireHydrant suggests the change events recorded just before it. These commands behave as they did in v0.0.15.

```sh
# Record an event
fhcli event --environment production --service web "hourly reconciliation task"
fhcli event --environment staging --identities git=git@github.com:acme/web:a0b0c0d --labels executor=jenkins "CI build succeeded"

# Run a command, time it, and record its result as an event
fhcli execute --environment staging --identities revision=a0a0a0 -- docker build .

# Write a config file for later runs (defaults to /tmp/firehydrant.cfg)
fhcli --config /etc/firehydrant.cfg --apiKey fhb-a0b010 --environment production --service web init
```

| Flag | Environment variable | Notes |
|---|---|---|
| `--environment` | `FH_ENVIRONMENT` | Environment ID or slug |
| `--service` | `FH_SERVICE` | Service ID or slug |
| `--identities` | `FH_IDENTITIES` | Comma-separated `key=value` pairs, e.g. `image=quay.io/acme/web:abc123` |
| `--labels` | `FH_LABELS` | Comma-separated `key=value` pairs |

Environments and services must match an ID or slug in your Service Catalog. Values that don't match are left blank on the event.

`event` and `execute` default to `--ignore-errors`. They print errors and exit 0, so a FireHydrant outage never fails your deploy. That includes a failing wrapped command under `execute`. Set `--ignore-errors=false` (or `ignoreErrors: false` in the config file) to fail instead: API errors exit non-zero and `execute` passes through the command's exit status. Missing credentials are always an error, and `execute` doesn't run the command without them.

## Incidents, services and teams

These are curated commands for the most common incident-time tasks. Incidents can be referenced by ID, number, or `#number`.

```sh
fhcli incidents list --status active
fhcli incidents triage 661          # details, alerts and AI summaries in one call
fhcli incidents context 661         # plus affected services, owning teams and on-call
fhcli incidents alerts|events|summary|transcript <incident>
fhcli incidents note <incident> "Rolled back web to v42"
fhcli incidents update|resolve <incident> ...

fhcli services list|get <service>
fhcli teams list|get <team>
fhcli teams oncall                  # current and next shift for each schedule
```

Run `fhcli <command> --help` for flags. Add `-i` for human-readable output.

## Any API operation: `fhcli api`

`fhcli api` exposes every operation in the FireHydrant public API. The commands are generated at runtime from the OpenAPI spec embedded in the binary.

```sh
fhcli search severity matrix                                   # find the operation for a task (offline)
fhcli api --help                                               # list API areas
fhcli api incidents list-incidents --status open               # query params are flags
fhcli api incidents get-incident 661                           # path params are positional
fhcli api incidents create-incident --name "DB down" --dry-run # print the request, send nothing
fhcli api incidents create-incident --data @incident.json      # full JSON body from a file (or @- for stdin)
```

- `--help` on an operation lists its parameters, body fields and documented response fields.
- DELETE operations require `--yes`.
- String body fields accept `@file` and `@-`, which is useful for long notes.
- To pick up endpoints added since your release, run `fhcli spec update`, which writes `~/.config/fhcli/spec.json`. `fhcli spec info` shows which spec is in use.

## Using fhcli from an AI agent

fhcli is designed to be driven by agents such as Claude Code:

- When stdout isn't a terminal, output is compact one-line JSON.
- Failures exit non-zero. They write a JSON object to stderr with `error`, plus `status`, `method`, `path` and `response` for API errors.
- `fhcli search` lets an agent find the right operation without browsing docs, and `--dry-run` lets it show a write before sending it.

Install the bundled Claude Code skill. It teaches the incident workflows, which commands to use, and which changes need a human's approval.

```sh
mkdir -p ~/.claude/skills/firehydrant
fhcli install --skills > ~/.claude/skills/firehydrant/SKILL.md
```

Re-run this after upgrading fhcli, since the skill ships with the binary.

## Output and exit codes

| | Default | Notes |
|---|---|---|
| Output | JSON (pretty in a terminal, compact when piped) | `-i` / `--interactive` for human-readable text |
| `incidents`, `services`, `teams`, `api`, `search` | exit 1 on any error | error JSON on stderr |
| `event`, `execute` | exit 0, even if the API or the wrapped command fails | With `--ignore-errors=false`, they exit non-zero on API errors and `execute` exits with the wrapped command's status |

## Configuration reference

| Setting | Flag | Environment | Config file key |
|---|---|---|---|
| API token | `--token`, `--apiKey` | `FIREHYDRANT_API_TOKEN`, `FH_API_KEY` | `apiKey` |
| API host | `--api-host`, `--apiHost` | `FH_API_HOST` | `apiHost` |
| Config file | `--config` | `FH_CONFIG_FILE` | |
| Ignore API errors | `--ignore-errors` | | `ignoreErrors` |
| Debug output | `--debug` | | `debug` |
| Environment, service, identities, labels | see [change events](#change-events-ci-and-infrastructure) | | `environment`, `service`, `identities`, `labels` |
| Token store | | `FHCLI_TOKEN_STORE=file` | |
| OAuth client | `login --client-id` | `FH_OAUTH_CLIENT_ID` | |

A flag beats an environment variable, and an environment variable beats the config file. Without `--config`, fhcli reads the first of `/etc/firehydrant.cfg`, `$HOME/firehydrant.cfg` and `/tmp/firehydrant.cfg` that exists. A config file's `apiHost` never redirects a stored login to another host. Pass `--api-host` explicitly for that.

## Development

```sh
make build       # ./fhcli, versioned from `git describe`
make test        # go vet + race tests
make test-live   # tests against the live API spec instead of the embedded one
make spec        # refresh the embedded spec (pkg/spec/spec.json)
make snapshot    # build all release archives locally (needs goreleaser v2)
```

Pull requests run CI: vet, tests, and a goreleaser snapshot build. See [RELEASING.md](RELEASING.md) to cut a release and [CHANGELOG.md](CHANGELOG.md) for what changed.
