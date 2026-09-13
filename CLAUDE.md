# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Cloudflare Worker, written in Go and compiled to WebAssembly with TinyGo, that notifies a Telegram
chat about GitHub Actions workflow runs. Pipeline (see `docs/infrastructure/overview-infras.md` for
full sequence diagrams):

```
GitHub App webhook (workflow_run) → Worker HTTP handler → D1 (idempotency check)
    → Cloudflare Queue → Queue consumer → GitHub REST API (enrichment) → Telegram
```

The Worker runs on [`syumai/workers-go`](https://github.com/syumai/workers-go), which is what makes
`net/http`, `database/sql`, and Cloudflare bindings (D1, Queues, secrets) usable from Go under
`GOOS=js GOARCH=wasm` / TinyGo. There is no officially-supported Go runtime for Cloudflare Workers
otherwise — this is a deliberate, somewhat unusual stack choice, not a default.

## Commands

```sh
npm install                      # also vendors TinyGo into ./.tools via postinstall (scripts/install-tinygo.sh)
cp .dev.vars.example .dev.vars   # fill in secrets for local dev — NOT .env, wrangler dev ignores .env
npm run db:migrations:local
npm run dev                      # wrangler dev

npm run build                    # go run workers-assets-gen && tinygo build -> ./build/
npm run deploy                   # wrangler deploy (runs the build step first via wrangler.toml's [build])
npm run db:migrations:remote
```

There is no test suite. The closest thing to lint/typecheck is:

```sh
gofmt -l .
GOOS=js GOARCH=wasm go vet ./...
```

`go vet`/`go build` **without** `GOOS=js GOARCH=wasm` will fail on any package that imports
`workers-go/cloudflare*` (they use `syscall/js`, which is excluded from normal darwin/linux builds).
This is expected — always type-check with that env pair, not a plain `go build ./...`.

TinyGo is required (not just `go build`) because Cloudflare enforces a script size limit that a
standard Go-toolchain wasm binary tends to exceed; TinyGo produces a much smaller binary. It's
vendored per-project into `.tools/tinygo` rather than installed globally — `npm run build`'s PATH
prepend picks it up automatically, so don't assume a global `tinygo` on PATH.

## Architecture notes

**Webhook handler** (`internal/github/webhook.go`): verifies `X-Hub-Signature-256` (HMAC-SHA256),
filters to `workflow_run` events with `action == "completed"`, and deduplicates on
`X-GitHub-Delivery` using D1's `deliveries` table — `INSERT ... ON CONFLICT (delivery_id) DO NOTHING`
then checking `RowsAffected()`, not a SELECT-then-INSERT (avoids a race, and D1's driver reports
`changes` via the result's `meta`, see `internal/github/api_client.go`-adjacent `internal/store/store.go`).
If enqueueing to the Cloudflare Queue fails after the delivery row was inserted, the row is deleted
so a GitHub retry of the same delivery isn't permanently swallowed as a false duplicate.

**Queue consumer** (`internal/notifier/consumer.go`): `max_batch_size = 1` in `wrangler.toml`, and
each message is acked or explicitly retried individually (`msg.Ack()` / `msg.Retry()`) rather than
relying on the batch handler's return value — this is what makes per-message DLQ semantics work.

**GitHub API enrichment** (`internal/github/api_client.go`, `app_auth.go`): the `workflow_run`
webhook payload alone doesn't carry per-job/step failure detail or GitHub-account identities for the
commit author / PR requester / merger, so the consumer authenticates as the GitHub App's installation
(JWT signed RS256 with the App's private key → installation access token) and calls the Jobs,
Commits, and Associated-PRs REST endpoints. This is **best-effort**: if the GitHub API call fails,
the consumer logs it and proceeds with `details == nil` rather than failing/retrying the whole
notification — the message just degrades to fields available from the webhook payload alone.

**Outbound HTTP** goes through `github.com/syumai/workers-go/cloudflare/fetch`
(`fetch.NewClient()` / `fetch.NewRequest()`), not a bare `net/http.Client` — there's no HTTP
transport implemented for outbound requests outside that package in this runtime.

**Telegram message format** (`internal/telegram/format.go`) is intentionally capped at 4–6 lines
(HTML `parse_mode`): title/conclusion, repo/branch, a commit-or-PR-title line that's itself a link
to the diff (PR's "Files changed" tab if the commit belongs to one, else the commit page — see
`changesURL`), an optional failed-jobs/steps line (only rendered for non-success conclusions, and
itself truncated to `maxFailedJobsShown`/`maxFailedStepsShown`), a triggered/cancelled/rerun line,
and a final `View run · View changes` link line. Keep new fields inside this budget rather than
adding lines.

**Config/secrets**: read via `cloudflare.Getenv(name)`, which returns both `[vars]` and secrets set
via `wrangler secret put` — there's no distinction at the Go call site. Deployed secrets are
independent of any local `.dev.vars`/`.env` file; changing a local file does nothing to what's live
until you re-run `wrangler secret put` (or the CD workflow does, on every deploy — see
`.github/workflows/cd.yml`). Required: `WEBHOOK_SECRET_GITHUB`, `TELEGRAM_BOT_TOKEN`,
`TELEGRAM_CHAT_ID`, `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY`.

`GITHUB_APP_INSTALLATION_ID` must be the **numeric** installation ID from
`github.com/settings/installations/<id>` (or the org variant) — not the App's OAuth Client ID
(`Iv23...`-style string on the App's General settings page). These are easy to conflate and the
installation-token exchange will 404 if the Client ID ends up there instead.

**Cloudflare resources are already provisioned** (not placeholders): D1 database, Queue, and DLQ
all named `github-workflow-telegram-notification` (DLQ has a `-dlq` suffix), IDs live in
`wrangler.toml`. GitHub App setup instructions are in `docs/instruction/github-app-setup.md`
(required permissions: Actions/Contents/Pull requests, all read-only; subscribed to `Workflow run`).

## Project layout

- `main.go` — wires the HTTP handler and queue consumer together, reads all env/secrets.
- `internal/github` — webhook verification + payload types + HTTP handler, and the GitHub App REST client.
- `internal/queue` — the JSON envelope (`delivery_id` + raw event) sent through Cloudflare Queues; carries the delivery ID since it only exists in a request header, not the webhook body.
- `internal/notifier` — queue consumer: enriches via the GitHub API, formats, sends to Telegram, updates D1.
- `internal/telegram` — Bot API client + message formatting.
- `internal/store` — D1 access (`deliveries` for idempotency/status, `workflow_runs` for history).
- `migrations/` — D1 schema (applied with `wrangler d1 migrations apply`, not the `schema.sql`-and-hope pattern).
