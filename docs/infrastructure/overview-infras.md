# Infrastructure overview

Architecture: **GitHub App + Cloudflare Worker + D1 + Cloudflare Queue + Telegram**.

The diagrams below walk through the pipeline stage by stage, then show the full end-to-end
sequence, followed by the failure/retry and duplicate-webhook cases.

## 1. Webhook delivery

```text
Developer          GitHub Actions        GitHub App         Cloudflare Worker
    │                     │                    │                     │
    │ git push            │                    │                     │
    ├────────────────────>│                    │                     │
    │                     │                    │                     │
    │                     │ workflow runs      │                     │
    │                     │ (build/test/...)   │                     │
    │                     │                    │                     │
    │                     │ workflow_run:      │                     │
    │                     │ completed          │                     │
    │                     ├───────────────────>│                     │
    │                     │                    │                     │
    │                     │                    │ POST /webhooks/github
    │                     │                    │ X-Hub-Signature-256 │
    │                     │                    │ X-GitHub-Delivery   │
    │                     │                    ├────────────────────>│
    │                     │                    │                     │
    │                     │                    │                     │ verify HMAC
    │                     │                    │                     │ signature
    │                     │                    │                     │
    │                     │                    │                     │ check
    │                     │                    │                     │ delivery_id (D1)
```

## 2. Persist + enqueue

```text
Cloudflare Worker        D1 Database          Cloudflare Queue
       │                      │                      │
       │ INSERT delivery      │                      │
       │ (status = pending)   │                      │
       ├─────────────────────>│                      │
       │                      │                      │
       │ queue.send(event)    │                      │
       ├─────────────────────────────────────────────>│
       │                      │                      │
       │<─────────────────────────────────────────────┤
       │ (enqueued)           │                      │
       │                      │                      │
       │ HTTP 200 to GitHub   │                      │
       ▼                      │                      │
```

## 3. Queue consumer stores the run

```text
Cloudflare Queue        Queue Consumer          D1 Database
       │                      │                      │
       │ deliver message      │                      │
       ├─────────────────────>│                      │
       │                      │                      │
       │                      │ parse workflow_run    │
       │                      │                      │
       │                      │ store run             │
       │                      ├─────────────────────>│
```

## 4. Send the Telegram notification

```text
Queue Consumer          D1 Database             Telegram
      │                       │                      │
      │ POST sendMessage                              │
      ├───────────────────────────────────────────────>│
      │                       │                      │
      │<───────────────────────────────────────────────┤
      │ 2xx                   │                      │
      │                       │                      │
      │ UPDATE delivery       │                      │
      │ (status = delivered)  │                      │
      ├──────────────────────>│                      │
```

## Full sequence

```text
┌───────────┐
│ Developer │
└─────┬─────┘
      │ git push
      ▼
┌─────────────────┐
│ GitHub Actions   │
└─────┬────────────┘
      │ workflow_run: completed
      ▼
┌─────────────────┐
│   GitHub App     │
│    webhook       │
└─────┬────────────┘
      │ POST /webhooks/github
      │ X-Hub-Signature-256
      │ X-GitHub-Delivery
      ▼
┌──────────────────────────┐
│    Cloudflare Worker      │
│                          │
│ 1. verify signature      │
│ 2. check event type      │
│ 3. check delivery_id     │
└─────────────┬─────────────┘
              │
      ┌───────┴────────┐
      ▼                ▼
┌───────────┐    ┌───────────────┐
│    D1     │    │ Cloudflare    │
│           │    │ Queue         │
│ idempotency│    │               │
│ + history │    │ workflow_run  │
└───────────┘    └───────┬───────┘
                          ▼
                 ┌──────────────────┐
                 │  Queue Consumer   │
                 └─────────┬─────────┘
                           │
                 ┌─────────┴─────────┐
                 ▼                   ▼
           ┌───────────┐      ┌──────────────┐
           │    D1      │      │  Telegram    │
           │            │      │  Bot API     │
           │ workflow_  │      └──────┬───────┘
           │ runs       │             │
           └───────────┘             ▼
                             ┌──────────────────┐
                             │ Telegram chat     │
                             │                  │
                             │ ✅ SUCCESS       │
                             │ ❌ FAILURE       │
                             └──────────────────┘
```

## Failure / retry

```text
Queue Consumer
      │
      │ POST Telegram
      ▼
  ┌──────────┐
  │ Telegram  │
  └────┬─────┘
       │ 5xx / timeout
       ▼
┌──────────────────┐
│ msg.Retry()       │
└─────────┬─────────┘
          │ automatic redelivery
          ▼
┌──────────────────┐
│ Queue Consumer    │
│ retry #1          │
└─────────┬─────────┘
          │
          ├── success ──> D1: status = delivered
          │
          └── fail
               │
               ▼
          retry #2 ...
               │
               ▼
          max retries exceeded
               │
               ▼
              DLQ
```

## Duplicate webhook

GitHub may redeliver the same webhook (e.g. after a timeout on its side), so the
`X-GitHub-Delivery` header is what makes the pipeline idempotent:

```text
GitHub
  │
  │ delivery_id = abc-123
  ▼
Cloudflare Worker
  │
  ▼
D1: INSERT delivery (delivery_id = abc-123)
  │      ON CONFLICT (delivery_id) DO NOTHING
  ▼
RowsAffected() == 0 → already exists
  │
  ├──────────────> HTTP 200 to GitHub
  │
  └── do not enqueue / do not notify Telegram
```

## Key point: no GitHub API call needed for basic status

The Worker does **not** need to call the GitHub API to know whether a run succeeded or failed —
the `workflow_run` webhook payload already carries that:

```text
workflow_run.conclusion
workflow_run.status
workflow_run.name
workflow_run.run_number
workflow_run.html_url
workflow_run.head_branch
workflow_run.head_sha
workflow_run.created_at
workflow_run.updated_at
```

The GitHub REST API is only called afterwards, best-effort, to enrich the message with detail the
webhook payload doesn't carry: which jobs/steps failed, and the GitHub identities of the commit
author / PR requester / merger. If that call fails, the notification still goes out using only the
fields above.

## Why go through D1 + Queue instead of calling Telegram directly

```text
GitHub Actions
      ↓
GitHub App
      ↓
Cloudflare Worker
      ↓
     D1 ── idempotency / history
      ↓
Cloudflare Queue
      ↓
Queue Consumer
      ↓
   Telegram
```

This is better than having the Worker call Telegram directly from the webhook handler: if Telegram
is slow or down, that doesn't fail the GitHub webhook delivery itself, and the Queue's built-in
retry mechanism handles the eventual delivery.
