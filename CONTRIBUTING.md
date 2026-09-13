# Contributing

## Prerequisites

- Go 1.24+
- [TinyGo](https://tinygo.org/getting-started/install/) 0.42.0+ — vendored automatically into
  `./.tools` by `npm install` (`scripts/install-tinygo.sh`), so a global install isn't required.
- Node.js and `npm`

## Setup

```sh
npm install                      # also vendors TinyGo into ./.tools via postinstall
cp .dev.vars.example .dev.vars   # fill in secrets for local dev — NOT .env, wrangler dev ignores .env
npm run db:migrations:local
npm run dev                      # wrangler dev
```

## Making changes

The stack is unusual: this is a Go program compiled to WebAssembly with TinyGo and run on
Cloudflare Workers via [`syumai/workers-go`](https://github.com/syumai/workers-go). Skim
`CLAUDE.md` and [docs/infrastructure/overview-infras.md](docs/infrastructure/overview-infras.md)
before making non-trivial changes — the webhook → D1 → Queue → Telegram pipeline has a few
deliberate design decisions (idempotency via `INSERT ... ON CONFLICT`, per-message queue acking,
best-effort GitHub API enrichment) that are easy to accidentally undo.

If you touch `internal/telegram/format.go`, keep the message within its 4–6 line budget — add
fields inside that budget rather than adding new lines.

## Checking your work

There is no test suite. The closest thing to lint/typecheck is:

```sh
gofmt -l .
GOOS=js GOARCH=wasm go vet ./...
```

Always type-check with `GOOS=js GOARCH=wasm` set — a plain `go build ./...` or `go vet ./...`
without that env pair will fail on any package that imports `workers-go/cloudflare*` (they use
`syscall/js`, which a normal darwin/linux build excludes). This is expected, not a bug in your
change.

Then confirm the build itself succeeds (this also runs `go vet`-equivalent compilation through
TinyGo):

```sh
npm run build
```

For webhook/consumer changes, run `npm run dev` and exercise the flow against a real GitHub App
test installation where possible — see
[docs/instruction/github-app-setup.md](docs/instruction/github-app-setup.md) for how to register
one.

## Submitting changes

- Keep commits focused; write commit messages that explain *why*, not just *what*.
- Run `gofmt` and the vet/build checks above before opening a PR.
- Don't add secrets, `.dev.vars`, or other local config to the diff — check `git status` /
  `git diff` before committing.
- Describe what you tested (or couldn't test, e.g. because it requires a live Telegram chat or
  GitHub App installation) in the PR description.
