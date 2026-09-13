# github-workflow-telegram-notification

Cloudflare Worker that monitors GitHub Actions workflow runs and sends execution results to Telegram.

Architecture: `GitHub App → Cloudflare Worker → D1 (idempotency + history) → Cloudflare Queue → Telegram`.

![img](./docs/img/image.png)

See [docs/infrastructure/overview-infras.md](docs/infrastructure/overview-infras.md) for the full
sequence diagrams and [docs/instruction/github-app-setup.md](docs/instruction/github-app-setup.md)
for how to register the GitHub App.

The worker is written in Go, compiled to WebAssembly with
[TinyGo](https://tinygo.org/) and run on Cloudflare Workers via
[`syumai/workers-go`](https://github.com/syumai/workers-go).

## Project layout

- [`main.go`](main.go) — wires up the HTTP handler and queue consumer.
- [`internal/github`](internal/github) — webhook signature verification, payload types, HTTP handler,
  and a GitHub App REST API client (failed jobs/steps, commit author, PR requester/merger).
- [`internal/queue`](internal/queue) — the envelope sent through Cloudflare Queues.
- [`internal/notifier`](internal/notifier) — queue consumer: formats and sends the Telegram message.
- [`internal/telegram`](internal/telegram) — Telegram Bot API client.
- [`internal/store`](internal/store) — D1 access (delivery idempotency + workflow run history).
- [`migrations`](migrations) — D1 schema.

## Development

Requirements: Go 1.24+, [TinyGo](https://tinygo.org/getting-started/install/) 0.42.0+, Node.js, `npm`.

```sh
npm install
cp .dev.vars.example .dev.vars   # fill in secrets for local dev
npm run db:migrations:local
npm run dev                      # wrangler dev
```

```sh
npm run build                    # go run workers-assets-gen && tinygo build
npm run deploy                   # wrangler deploy
```

Deploys to `main` run automatically via [`.github/workflows/cd.yml`](.github/workflows/cd.yml),
using the `CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID` / `TELEGRAM_BOT_TOKEN` /
`TELEGRAM_CHAT_ID` / `WEBHOOK_SECRET_GITHUB` / `GITHUB_APP_ID` / `GITHUB_APP_INSTALLATION_ID` /
`GITHUB_APP_PRIVATE_KEY` repository secrets. See
[docs/instruction/github-app-setup.md](docs/instruction/github-app-setup.md) for where the
GitHub App values come from.
