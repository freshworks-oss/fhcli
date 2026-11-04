# Changelog

This project follows [Semantic Versioning](https://semver.org/). Before 1.0, a minor bump (0.x.0) can include behaviour changes; each one is listed under **Changed**.

## [Unreleased]

## [0.1.0]

The first release since v0.0.15 (August 2025). fhcli grows from a change-event submitter into a general FireHydrant CLI for people and AI agents. Change-event usage from CI is backwards compatible.

### Added

- **Browser login.** `fhcli login`, `fhcli whoami` and `fhcli logout` use OAuth (authorization code with PKCE) and store the login in the macOS Keychain, or in a `0600` file on Linux. Tokens refresh automatically. Regions are selected with `--region us|eu|au|in|staging`; built-in clients exist for `us` and `staging` only, so other regions need `--client-id` for now.
- **Every public API operation.** `fhcli api <area> <operation>` covers all 479 operations, generated from the embedded OpenAPI spec. Parameters are typed flags, `--data` takes a JSON body (with `@file` and `@-`), `--dry-run` prints the request without sending it, and DELETE requires `--yes`. `--help` shows request and documented response fields.
- **Offline search.** `fhcli search <words>` finds the operation for a task and lists its required inputs.
- **Spec management.** `fhcli spec update` fetches the latest spec without a new release, and `fhcli spec info` shows which spec is in use.
- **Incident commands.** `fhcli incidents list|get|triage|context|alerts|events|summary|transcript|note|chat|update|resolve`.
- **Catalog and on-call.** `fhcli services list|get` and `fhcli teams list|get|oncall`.
- **Incident references.** Incidents can be referenced by number or `#number` as well as by ID, including in `fhcli api`.
- **Claude Code skill.** `fhcli install --skills` prints a Claude Code skill for incident workflows.
- **Long-form names for global flags.** `--token`, `--api-host` and `--ignore-errors` sit alongside the existing `--apiKey`, `--apiHost` and `--ignoreErrors`, and `FIREHYDRANT_API_TOKEN` is accepted alongside `FH_API_KEY`.

### Changed

- **Global flag placement.** Global flags may now come after the subcommand, e.g. `fhcli event --apiKey ...`.
- **Output and exit codes for the new commands.** They print JSON, pretty in a terminal and compact when piped, and exit non-zero on failure with a JSON error object on stderr. `event`, `execute` and `init` keep their v0.0.15 text output and `--ignore-errors` default.
- **Config file settings now apply.** `apiKey`, `apiHost`, `ignoreErrors`, `environment`, `service`, `identities` and `labels` from the config file are applied, with flags and environment variables taking precedence. A config file's `apiHost` never redirects a stored login.
- **`execute` with an expired login.** If a stored login can't refresh, `execute` still runs the wrapped command. Missing credentials still fail before running it, as in v0.0.15.
- **Message wording.** Error messages for API failures and usage mistakes are worded differently.
- **Version source.** The version reported by `fhcli --version` now comes from the release tag.

### Internal

- Upgraded to urfave/cli v3 and firehydrant-go-sdk v1.8.5. Dropped the go-openapi dependency.
- Releases are built by goreleaser v2 from GitHub Actions. Pull requests run vet, race tests and a release dry run. CircleCI was removed.

[Unreleased]: https://github.com/freshworks-oss/fhcli/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/freshworks-oss/fhcli/compare/v0.0.15...v0.1.0
