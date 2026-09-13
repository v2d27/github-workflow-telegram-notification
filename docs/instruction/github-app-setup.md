# GitHub App setup

This worker is notified of workflow runs via a GitHub App webhook, per
[overview-infras.md](./overview-infras.md). The `workflow_run` webhook payload
alone covers conclusion, branch, commit SHA, and who triggered/reran/cancelled
the run — but to report **which jobs/steps failed** and **who authored the
commit or requested/merged the PR**, the worker calls back into the GitHub
REST API, authenticated as this App's installation. That means the App does
need a private key this time.

## 1. Create the App

GitHub Settings → Developer settings → GitHub Apps → New GitHub App.

- **GitHub App name**: `github-workflow-telegram-notification` (or similar)
- **Homepage URL**: the repository URL is fine.
- **Webhook**:
  - Active: checked
  - Webhook URL: `https://<your-worker-subdomain>.workers.dev/webhooks/github`
    (the exact `*.workers.dev` subdomain is only known after the first
    `wrangler deploy` — come back and fill this in once you have it, or use
    a custom domain if you've mapped one to the worker)
  - Webhook secret: generate a strong random value, e.g. `openssl rand -hex 32`,
    and keep it — you'll need it in step 3.
- **Repository permissions**:
  - Actions → Read-only (required to receive `workflow_run` events and to
    list a run's jobs/steps)
  - Contents → Read-only (required to look up the commit author)
  - Pull requests → Read-only (required to find the PR associated with a
    commit and who merged it)
- **Subscribe to events**: check `Workflow run`.
- **Where can this GitHub App be installed?**: "Only on this account" is
  sufficient unless you plan to reuse it across orgs.

Save, then **Generate a private key** (App settings, near the bottom) and
download the `.pem` file — you'll need its contents in step 3. Keep it
somewhere safe; GitHub only lets you download it once per key.

## 2. Install the App

App settings → Install App → choose the repositories that should send
`workflow_run` notifications (e.g. this repo, or any repos you want tracked).

After installing, the installation ID is the numeric ID in the URL of the
installation's settings page, e.g.
`https://github.com/settings/installations/<installation_id>` (or
`https://github.com/organizations/<org>/settings/installations/<installation_id>`
for an organization install). This is **not** the same as the App's OAuth
Client ID (the `Iv23...`-style string on the General settings page) —
`GITHUB_APP_INSTALLATION_ID` must be the plain numeric ID.

The App ID itself is shown on the App's General settings page (top of the
page, "App ID").

## 3. Wire the App's credentials into the worker

Four values need to reach the worker as secrets, matching the pattern
already used for `CLOUDFLARE_API_TOKEN` / `TELEGRAM_BOT_TOKEN` / etc. in
`platform-infrastructure/profile/github/repositories/github-workflow-telegram-notification/secrets.json`:

| Secret | Source |
| --- | --- |
| `WEBHOOK_SECRET_GITHUB` | The webhook secret you generated in step 1. |
| `GITHUB_APP_ID` | The App's "App ID" from its General settings page. |
| `GITHUB_APP_INSTALLATION_ID` | The numeric ID from the installation settings URL, step 2. |
| `GITHUB_APP_PRIVATE_KEY` | The full contents of the `.pem` file from step 1, as-is (including the `-----BEGIN/END RSA PRIVATE KEY-----` lines). |

The CD workflow (`.github/workflows/cd.yml`) pushes all four to the deployed
worker on every deploy via `wrangler secret put`.

For local development, copy `.dev.vars.example` to `.dev.vars` and fill in
the same values (`.dev.vars` is gitignored). For the private key, keep the
PEM's newlines intact in the file.

## 4. Verify

After deploying, check the App's **Advanced** tab → **Recent Deliveries** for
a `workflow_run` delivery. A `200` response confirms the worker accepted and
enqueued it; check the Telegram chat for the notification shortly after —
it should include the failed job/step names (for a failing run) and the
commit author / PR requester / merger.
