Với kiến trúc **GitHub App + Cloudflare Worker + D1 + Queue + Telegram**, sequence diagram dạng text như sau:

use go language

```text
Developer        GitHub Actions       GitHub App        Cloudflare Worker
    │                  │                  │                    │
    │  git push        │                  │                    │
    ├─────────────────>│                  │                    │
    │                  │                  │                    │
    │                  │ Workflow runs    │                    │
    │                  ├──────────────────┤                    │
    │                  │                  │                    │
    │                  │ Workflow         │                    │
    │                  │ completed        │                    │
    │                  │                  │                    │
    │                  │  workflow_run    │                    │
    │                  │  completed       │                    │
    │                  ├─────────────────>│                    │
    │                  │                  │                    │
    │                  │                  │ POST webhook        │
    │                  │                  │ X-Hub-Signature-256 │
    │                  │                  ├───────────────────>│
    │                  │                  │                    │
    │                  │                  │                    │ Verify HMAC
    │                  │                  │                    │
    │                  │                  │                    │ Check
    │                  │                  │                    │ delivery_id
    │                  │                  │                    │
```

### Tiếp theo: D1 + Queue

```text
Cloudflare Worker        D1 Database          Cloudflare Queue
       │                      │                      │
       │ INSERT delivery      │                      │
       │ status = pending     │                      │
       ├─────────────────────>│                      │
       │                      │                      │
       │ Queue.send(event)    │                      │
       ├────────────────────────────────────────────>│
       │                      │                      │
       │ HTTP 200             │                      │
       ├──────────────────────┐                      │
       │                      │                      │
       │<─────────────────────┘                      │
       │                                             │
       │                                             │
       │                         Queue Consumer      │
       │                              │              │
       │                              │ receive      │
       │                              │<─────────────┤
       │                              │              │
       │                              │ Parse        │
       │                              │ workflow_run │
       │                              │              │
       │                              ▼              │
       │                         D1 Database         │
       │                              │              │
       │                              │ store run    │
       │                              │              │
```

### Gửi notification Telegram

```text
Queue Consumer       D1 Database          Telegram
      │                    │                  │
      │ Save workflow run  │                  │
      ├───────────────────>│                  │
      │                    │                  │
      │                    │                  │
      │ POST Telegram Webhook                  │
      ├──────────────────────────────────────>│
      │                    │                  │
      │                    │       204        │
      │<──────────────────────────────────────┤
      │                    │                  │
      │ UPDATE delivery   │                  │
      │ status=delivered  │                  │
      ├───────────────────>│                  │
      │                    │                  │
      ▼                    ▼                  ▼
```

## Full sequence

```text
┌──────────┐
│ Developer│
└────┬─────┘
     │ git push
     ▼
┌─────────────────┐
│ GitHub Actions  │
└────┬────────────┘
     │
     │ workflow_run: completed
     ▼
┌─────────────────┐
│   GitHub App    │
│    Webhook      │
└────┬────────────┘
     │
     │ POST /webhooks/github
     │ X-Hub-Signature-256
     │ X-GitHub-Delivery
     ▼
┌─────────────────────────┐
│    Cloudflare Worker    │
│                         │
│  1. Verify signature    │
│  2. Check event type    │
│  3. Check delivery_id   │
└───────────┬─────────────┘
            │
            ├───────────────────────┐
            │                       │
            ▼                       ▼
      ┌───────────┐          ┌──────────────┐
      │    D1     │          │ Cloudflare   │
      │           │          │    Queue     │
      │ idempotency│          │              │
      │ + history │          │ workflow_run │
      └───────────┘          └──────┬───────┘
                                    │
                                    ▼
                           ┌──────────────────┐
                           │ Queue Consumer   │
                           │     Worker       │
                           └────────┬─────────┘
                                    │
                  ┌─────────────────┴─────────────────┐
                  │                                   │
                  ▼                                   ▼
             ┌─────────┐                       ┌──────────┐
             │   D1    │                       │ Telegram  │
             │         │                       │ Webhook  │
             │workflow │                       └────┬─────┘
             │  runs   │                            │
             └─────────┘                            │
                                                    ▼
                                           ┌─────────────────┐
                                           │ Telegram Channel │
                                           │                 │
                                           │ ✅ SUCCESS      │
                                           │ ❌ FAILURE      │
                                           └─────────────────┘
```

### Failure / retry

```text
Queue Consumer
      │
      │ POST Telegram
      ▼
  ┌─────────┐
  │ Telegram │
  └────┬────┘
       │
       │ 5xx / timeout
       ▼
┌──────────────────┐
│ Queue message    │
│ fails            │
└────────┬─────────┘
         │
         │ automatic retry
         ▼
┌──────────────────┐
│ Queue Consumer   │
│ retry #1         │
└────────┬─────────┘
         │
         ├── success ──> D1: delivered
         │
         └── fail
              │
              ▼
         retry #2 ...
              │
              ▼
         max retries
              │
              ▼
             DLQ
```

### Duplicate webhook

GitHub có thể gửi lại webhook, nên `X-GitHub-Delivery` rất quan trọng:

```text
GitHub
  │
  │ delivery_id = abc-123
  ▼
Worker
  │
  ▼
D1
  │
  │ SELECT delivery_id = abc-123
  ▼
Already exists
  │
  ├──────────────> HTTP 200
  │
  └── DO NOT send Telegram
```

**Điểm quan trọng:** Worker **không cần gọi GitHub API** để biết Success/Failure. Payload `workflow_run` đã chứa:

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

Vì vậy flow production mình khuyến nghị là:

```text
GitHub Actions
      ↓
GitHub App
      ↓
Cloudflare Worker
      ↓
    D1 ─── idempotency/history
      ↓
Cloudflare Queue
      ↓
Queue Consumer
      ↓
   Telegram
```

Cách này tốt hơn việc Worker gọi Telegram trực tiếp vì **Telegram chậm/down sẽ không làm GitHub webhook request thất bại**, và Queue có cơ chế retry.
