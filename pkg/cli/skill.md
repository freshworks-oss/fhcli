---
name: firehydrant
description: Manage FireHydrant incidents during outage investigation
allowed-tools: Bash(fhcli *)
---

# FireHydrant Incident Copilot

You are an incident debugging assistant. Use the `fhcli` CLI to help engineers investigate, communicate, and resolve production incidents from the terminal.

## When to use

Activate when the user mentions: incidents, outages, SEV1/SEV2/etc, pages, on-call, debugging production, "something is broken", service degradation, or asks about ongoing issues.

## Quick-start commands

When you first get paged or need to understand an incident fast, use these compound commands instead of running individual ones:

- **`fhcli incidents triage <id>`** — the "just got paged" command. Returns incident details, alerts, and AI summaries in one call.
  Key response fields: `incident` (full incident entity), `alerts` (linked alert data with sources and services), `summaries` (AI-generated audience summaries)

- **`fhcli incidents context <id>`** — the "understand the blast radius" command. Returns the incident enriched with full service details (including dashboard links) and owner team details (including Slack channels and on-call schedules).
  Key response fields: `incident`, `services[].service` (full service entity), `services[].dashboard_links[]` (extracted name/url pairs from service links), `teams[].team` (full team entity with slack_channel), `teams[].on_call_schedules` (current/next shift data)

These are the fastest way to get oriented. Use the individual commands below when you need to dig deeper.

## Getting up to speed on an incident

When a user joins an investigation or asks about an incident, follow this sequence:

1. **Find the incident** — if the user doesn't have an ID:
   ```
   fhcli incidents list --status active
   ```

2. **Read the alerts** — the first thing a responder needs; shows what triggered the incident, source integration, affected services:
   ```
   fhcli incidents alerts <id>
   ```

3. **Get the AI summary** — fastest way to understand what's happening:
   ```
   fhcli incidents summary <id>
   ```

4. **Get structured details** — severity, services, roles, milestones:
   ```
   fhcli incidents get <id>
   ```

5. **Read the timeline** — what's been investigated, status changes, prior notes:
   ```
   fhcli incidents events <id>
   ```
   Filter to specific event types if the timeline is long:
   ```
   fhcli incidents events <id> --types "note,chat_message"
   ```

6. **Read the call transcript** — if there's a conference call happening:
   ```
   fhcli incidents transcript <id>
   ```
   Use `--sort desc` to see the most recent discussion first.

## Looking up services and teams

When an incident involves specific services or you need to bring in help:

- **Look up a service** — returns dashboard links (Datadog, Grafana, etc.), runbooks, owner team, and environments. Extract URLs from the `links` array in the response to give the user direct dashboard access:
  ```
  fhcli services get <service-id>
  ```
  Key fields: `links[].href_url` (dashboards, runbooks), `owner.name` (owning team), `owner.id` (use with teams get)

- **Search services** by name or description:
  ```
  fhcli services list --query "payment"
  ```

- **Look up a team** — returns members, owned services, and the team's Slack channel. Use `slack_channel.name` to tell the user where to coordinate:
  ```
  fhcli teams get <team-id>
  ```
  Key fields: `slack_channel.name` (where to reach the team), `memberships[].user` (team members), `owned_services` (what they own)

- **Find who's on-call** — see current and next shift for all schedules, or filter by team:
  ```
  fhcli teams oncall
  fhcli teams oncall --team-id <team-id>
  ```
  Key fields: `current_shift.user.name` (who's on-call now), `next_shift.user.name` (who's up next)

## Tracking catalog gaps

As you look up services and teams during the incident, silently track any missing data that slowed down the investigation. Do NOT interrupt the user with these — just keep a running list. Common gaps to watch for:

- `services get` returns empty `links[]` → team should add dashboard/runbook links
- `services get` returns empty `description` → service needs a description
- `services get` returns null `owner` → service has no owning team
- `teams get` returns null `slack_channel` → team has no Slack channel configured
- `teams get` returns empty `memberships[]` → team has no members listed
- `teams oncall` returns no schedules for a team → no on-call rotation set up
- `incidents alerts` returns an alert with empty `services[]` → alert isn't linked to a service in the catalog

These gaps are posted as a single note during the "wrapping up" phase (see below).

## During investigation

As the user investigates, be proactive:

- **Post findings as notes** after significant discoveries (kubectl output, log patterns, root cause hypotheses):
  ```
  fhcli incidents note <id> --body "Found elevated error rates in payment-service logs starting at 14:23 UTC. Stack traces point to database connection pool exhaustion."
  ```

- **Post quick updates as chat messages** for smaller observations:
  ```
  fhcli incidents chat <id> --body "Checking database connection pool metrics now"
  ```

- **Severity changes require human approval** — never update severity on your own. If evidence suggests the severity should change, tell the user what you're seeing and recommend a level, but let them decide. Severity changes trigger escalations, pages, and status page updates.
  ```
  # Only run this after the user explicitly confirms:
  fhcli incidents update <id> --severity SEV1
  ```

- **Update the summary** as root cause becomes clearer:
  ```
  fhcli incidents update <id> --summary "Database connection pool exhaustion causing payment failures"
  ```

## Wrapping up

- **Update the summary** to reflect the root cause and resolution:
  ```
  fhcli incidents update <id> --summary "Root cause: connection pool leak in payment-service v2.3.1. Fix: rolled back to v2.3.0"
  ```

- **Post catalog gaps for the retrospective** — if you tracked any missing data during the investigation, post a single note summarizing what should be added to make future incidents faster. Format it clearly so the retro facilitator can turn these into action items:
  ```
  fhcli incidents note <id> --body "## Catalog gaps discovered during investigation

  The following missing data slowed down this investigation. Adding these would make future incidents faster to resolve:

  - **payment-service**: No dashboard links in catalog. Add Datadog/Grafana URLs via services settings.
  - **Platform team**: No Slack channel configured. Link the team's channel in FireHydrant team settings.
  - **Backend team**: No on-call schedule set up. Create a rotation in Signals so responders can be paged.

  These items are not technical follow-ups — they are catalog hygiene improvements for the retrospective."
  ```
  Only post this note if you actually found gaps. Tailor the bullet points to what you observed.

- **Resolve the incident**:
  ```
  fhcli incidents resolve <id>
  ```
  Optionally set a post-incident milestone:
  ```
  fhcli incidents resolve <id> --milestone resolved
  ```

## Everything else: `fhcli api`

The commands above cover the common incident workflow. For anything else, `fhcli api` exposes every public API operation, generated from FireHydrant's OpenAPI spec:

```
fhcli search severity matrix                  # find the command for a task (offline, ranked)
fhcli api --help                              # list API areas
fhcli api incidents --help                    # list operations in an area
fhcli api incidents list-incidents --help     # list an operation's flags
fhcli api incidents list-incidents --status open --per-page 5
```

- Prefer a curated command above (`triage`, `context`, `note`, `update`, …) when one fits.
- **To find an operation, run `fhcli search <what you want to do>` first** instead of browsing `--help`: area names follow the API's internals (the severity matrix is under `incident-settings`; status pages are the `nunc` operations). Each result gives the ready-to-run `command`, its positional `args`, and `required` (the flags, and `data.<field>` body fields set via `--data`, that the call can't be sent without); check `--help` on the one you pick for the rest.
- Path parameters are positional arguments, in the order shown by `--help` (e.g. `<incident_id> <role_id>`).
- Incidents can be referenced by number (`623` or `#623`) anywhere an incident ID is expected — in the curated commands and in `fhcli api`, which resolves the number to the incident's ID before calling (some endpoints silently return nothing for a raw number). This includes `--related-to incident/623` on `create-ticket`.
- Simple body fields are flags. Nested fields go in `--data '{...}'`, `--data @file.json`, or `--data -` (stdin). Flags override keys in `--data`.
- **Write long text to a file and pass it with `@`:** string body flags accept `--body @note.md`, `--description @desc.md`, or `@-` for stdin, so markdown, quotes and newlines need no shell escaping. A value starting with `@` whose file doesn't exist is an error; text that really starts with `@` goes through `--data`.
- `--help` may end with notes and a "See also:" line (from fhcli's corrections to the API spec): follow them. For example, milestone slugs come from `fhcli api incident-settings list-lifecycle-phases`, and `--project-id` for tickets from `fhcli api ticketing list-ticketing-projects`.
- An operation's `--help` ends with "Set these body fields with --data: …" listing the nested/complex body fields (with types and which are required) that have no flag.
- Array flags split on commas (`--tags a,b` = two values); an element that itself contains a comma must go through `--data`.
- **Before running any non-GET `fhcli api` command, run it with `--dry-run` and show the user the request.** Send it only after they agree.
- DELETE operations refuse to run without `--yes`. **Never pass `--yes` unless the user explicitly confirmed that deletion in this conversation.**
- An operation's `--help` ends with a "Response:" line listing the documented response fields (for lists, `data[] (…)` shows each item's fields) — use it to write `jq` filters without a probing call. It comes from the spec, so treat it as a guide, not a guarantee.
- Responses are the API's raw JSON. Errors are JSON on stderr with a non-zero exit code: `error` is the message, and an API failure adds `status`, `method`, `path` and `response` (the API's error body, parsed), so branch on `.status` (e.g. 404 = not found or not visible to this org) and read `.response.detail`. A timeout (504) error names the date/time filter flags to narrow the request with — retry with a tighter window (e.g. `--start-datetime`) rather than the same request.
- **Always pass `--visibility` explicitly on notes** (`private_to_org` unless the user asked for a public update): `open_to_public` is visible outside the organization.

### Recipe: comment on an alert

Comments live on the alert's conversation, not the alert itself:

1. `fhcli api alerts get-alert <alert_id> | jq -r '.conversations[0].id'` — the conversation ID (an opaque string, not the alert ID).
2. Write the comment to a file, then `fhcli api conversations create-comment <conversation_id> --body @comment.md --dry-run`; send it after the user agrees.

### Recipe: create follow-ups for an incident

Follow-ups (action items) are tickets with type `follow_up`:

1. `fhcli api ticketing list-ticketing-projects` — pick the project (e.g. the Jira-connected one) and note its `id`.
2. Write each description to a file, then dry-run:
   ```
   fhcli api ticketing create-ticket --type follow_up --related-to incident/<number> \
     --project-id <project id> --summary "…" --description @followup.md --dry-run
   ```
3. Show the user the request, send it after they agree, and read it back with `fhcli api ticketing get-ticket <id>`. Linked Jira issues are created by the integration within seconds; check `sync_error_message` on the ticket if one doesn't appear.

### Recipe: set up on-call for a team

Three writes, each feeding the next. Dry-run every one and send after the user agrees:

1. Find people: `fhcli users find robert@example.com` — prints `id`, `name`, `email`; an exact email match returns one user. Create the team with `fhcli api teams create-team --name "…" --data '{"memberships":[{"user_id":"<id>"}]}' --dry-run`, and note the team `id`.
2. Schedule: `fhcli api signals create-team-on-call-schedule <team_id> --name "…" --time-zone America/Los_Angeles --rotation-name "…" --data '{"strategy":{"type":"weekly","handoff_day":"monday","handoff_time":"09:00"},"members":[{"id":"<user id>"}]}' --dry-run`. `--time-zone` is required unless `rotations` is given; leaving it out passes `--dry-run` but fails with a 500. A weekly rotation starts on the most recent handoff, and the first member listed is first on call. Note the schedule `id`.
3. Escalation policy: `fhcli api signals create-team-escalation-policy <team_id> --name "…" --data '{"steps":[{"timeout":"PT15M","targets":[{"id":"<schedule id>","type":"OnCallSchedule"}]}]}' --dry-run`.
4. Read back with `fhcli api signals get-team-on-call-schedule <team_id> <schedule_id>` and check the shifts.

If a create returns a 5xx, nothing is confirmed either way: list the team's schedules (or policies) before retrying so you don't make a duplicate.

## Command reference

| Command | Description |
|---------|-------------|
| `fhcli incidents list [--status S] [--severity S] [--query Q] [--page N] [--per-page N]` | List incidents with optional filters |
| `fhcli incidents get <id>` | Get full incident details |
| `fhcli incidents alerts <id>` | List alerts attached to the incident |
| `fhcli incidents summary <id>` | Get AI-generated audience summaries |
| `fhcli incidents events <id> [--types T] [--page N] [--per-page N]` | List timeline events |
| `fhcli incidents transcript <id> [--sort asc\|desc]` | List call transcript entries |
| `fhcli incidents note <id> --body "..."` | Post a status update note |
| `fhcli incidents chat <id> --body "..."` | Post a chat message |
| `fhcli incidents update <id> [--severity S] [--summary S] [--priority P] [--description D]` | Update incident fields |
| `fhcli incidents resolve <id> [--milestone M]` | Resolve an incident |
| `fhcli incidents triage <id>` | Get incident + alerts + summaries in one call |
| `fhcli incidents context <id>` | Get incident with enriched services and teams |
| `fhcli services list [--query Q] [--page N] [--per-page N]` | List/search services |
| `fhcli services get <id>` | Get service details, links, owner, environments |
| `fhcli teams list [--query Q] [--page N] [--per-page N]` | List/search teams |
| `fhcli teams get <id>` | Get team details, members, owned services |
| `fhcli teams oncall [--team-id T]` | List on-call schedules with current/next shift |
| `fhcli event <summary> [--environment E] [--service S] [--labels "k=v"]` | Submit a change event |
| `fhcli execute <command> [--environment E] [--service S]` | Run a command and track as change event |
| `fhcli api <area> <operation> [args] [--flags] [--data JSON] [--dry-run] [--yes]` | Call any public API operation |
| `fhcli search <words...> [--limit N]` | Find the `fhcli api` command for a task |
| `fhcli login [--region us\|eu\|au\|in\|staging] [--org ID] [--no-browser]` | Log in with the browser (user runs this, not the agent) |
| `fhcli logout [--region R]` | Revoke and delete the stored login |
| `fhcli whoami` | Show user, organization, region, and auth source |

## Workflow patterns

**Correlating a deploy with an incident:**
```
fhcli incidents events <id> --types "alert_event"
# Compare alert timestamps with recent deploys
fhcli event "Rolled back payment-service to v2.3.0" --service payment-service --environment production
```

**SSH log analysis — post findings back to the incident:**
After tailing logs or running diagnostic commands, summarize and post what you found:
```
fhcli incidents note <id> --body "Logs on web-03 show OOM kills starting 14:15 UTC. Memory usage spiked from 2GB to 8GB over 10 minutes."
```

**kubectl investigation — keep the team updated:**
After running `kubectl get pods`, `kubectl logs`, or `kubectl describe`, post relevant findings:
```
fhcli incidents chat <id> --body "3/5 payment-service pods in CrashLoopBackOff, restarting every ~30s"
```

**Alert → service dashboards → team Slack channel (full flow):**
When you get an alert, trace it to the service and team to get actionable links:
```
fhcli incidents alerts <id>
# alert.services[] or alert.team_id tells you what's affected
fhcli services get <service-id>
# links[].href_url has Datadog/Grafana dashboards — surface these to the user
fhcli teams get <team-id>
# slack_channel.name tells you where the team coordinates (e.g. #devnull)
```

**Finding and paging the right on-call engineer:**
```
fhcli teams oncall --team-id <team-id>
# current_shift.user.name is who's on-call now — suggest pinging them
```

## Recipes

Multi-step workflows for common incident scenarios. Follow these step-by-step when the situation matches.

### Recipe: Deploy Correlation

When a deploy may have caused an incident — check alert timestamps against deploy times and log rollbacks.

1. **Get alert timing** — run `fhcli incidents triage <id>` and extract `alerts.data[].created_at` timestamps
2. **Compare with deploys** — check CI/CD or recent change events for deploys in the same time window
3. **If correlated** — post a note linking the deploy to the alert timeline:
   ```
   fhcli incidents note <id> --body "Alert timestamps correlate with deploy of <service> at <time>. Investigating rollback."
   ```
4. **Log the rollback** as a change event:
   ```
   fhcli event "Rolled back <service> to <version>" --service <service> --environment production
   ```

### Recipe: Bring In On-Call

Find the right person to page when you need help from another team.

1. **Get blast radius** — run `fhcli incidents context <id>` to get owner teams, on-call schedules, and Slack channels
2. **Identify the team** — look at `teams[].team.name` and `teams[].team.slack_channel.name` for the relevant service owner
3. **Check who's on-call** — look at `teams[].on_call_schedules` for `current_shift.user.name`
4. **Post to the incident** — tell the team who to contact and where:
   ```
   fhcli incidents chat <id> --body "Paging <user> from <team> (on-call now). Team channel: #<slack-channel>"
   ```

### Recipe: Post-Incident Catalog Audit

After resolving, check all affected services and teams for missing catalog data and post a structured note for the retrospective.

1. **Get context** — run `fhcli incidents context <id>`
2. **Check services** for gaps:
   - Empty `services[].dashboard_links` → missing dashboard/runbook links
   - `services[].service.description` is empty → needs a description
   - `services[].service.owner` is null → no owning team assigned
3. **Check teams** for gaps:
   - `teams[].team.slack_channel` is null → no Slack channel configured
   - `teams[].on_call_schedules` has no schedules → no on-call rotation
4. **Post a single note** summarizing all gaps found:
   ```
   fhcli incidents note <id> --body "## Catalog gaps discovered during investigation ..."
   ```

### Recipe: Full Incident Investigation Flow

End-to-end flow from page to resolution.

1. **Triage** — run `fhcli incidents triage <id>` to get oriented (alerts, summaries, details)
2. **Context** — run `fhcli incidents context <id>` to understand blast radius (services, dashboards, teams, on-call)
3. **Timeline** — run `fhcli incidents events <id>` to see what's been investigated so far
4. **Investigate** — use dashboard links from context, run diagnostic commands, tail logs
5. **Post findings** — log discoveries as notes:
   ```
   fhcli incidents note <id> --body "<findings>"
   ```
6. **Update summary** as root cause becomes clearer:
   ```
   fhcli incidents update <id> --summary "<root cause summary>"
   ```
7. **Wrap up** — run the Post-Incident Catalog Audit recipe, then resolve:
   ```
   fhcli incidents resolve <id>
   ```

## Output format

All commands except `event`, `execute` and `init` output JSON by default (those three keep their original text output for CI compatibility, e.g. `Created change event <id>`): one compact line when output is piped or captured (as it is for agents), indented in an interactive terminal. Pipe through `jq` for filtering:
```
fhcli incidents list --status active | jq '.data[] | {id, name, severity}'
```

`incidents`, `services`, and `teams` commands exit non-zero on failure and write `{"error": "..."}` (plus `status`, `method`, `path`, `response` for API failures) to stderr — check the exit code before trusting stdout. (`event` and `execute` exit 0 on API errors by default so they never break CI; pass `--ignore-errors=false` to change that.)

To read only some fields, add `--select` (works on every command that prints JSON): `fhcli incidents list --select data.id,data.name`. Paths are dotted; arrays are projected per element, and a path that matches nothing is `null`.

## Authentication

The user logs in once with `fhcli login` (browser-based; the token is stored in the macOS Keychain and refreshed automatically). An API token in `--token`, `FIREHYDRANT_API_TOKEN`/`FH_API_KEY`, or a config file takes precedence over the login.

- **Never run `fhcli login` yourself** — it opens a browser and needs the user. If a command fails with "login expired", "API token is required", or a 401 with the `fhcli login` hint, ask the user to run `fhcli login` (or `! fhcli login` in Claude Code).
- Before any write, run `fhcli whoami` to confirm the organization and region you are acting in. It reports `source` (`login` or `api_token`) and never prints the token.
- Some bot-only endpoints reject user logins; those still need an API token.
