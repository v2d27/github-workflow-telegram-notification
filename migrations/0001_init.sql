-- Migration number: 0001 	 2026-09-13T00:00:00.000Z

-- Idempotency + delivery status for each GitHub webhook, keyed by the
-- X-GitHub-Delivery header GitHub sends on every request (including
-- retries of the same delivery).
CREATE TABLE deliveries (
    delivery_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'pending', -- pending | delivered | failed
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- History of workflow_run events, written by the queue consumer once a
-- delivery has been parsed.
CREATE TABLE workflow_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    delivery_id TEXT NOT NULL REFERENCES deliveries (delivery_id),
    repository TEXT NOT NULL,
    workflow_name TEXT NOT NULL,
    run_id INTEGER NOT NULL,
    run_number INTEGER NOT NULL,
    status TEXT NOT NULL,
    conclusion TEXT,
    head_branch TEXT,
    head_sha TEXT,
    actor TEXT,
    html_url TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_workflow_runs_delivery_id ON workflow_runs (delivery_id);
CREATE INDEX idx_workflow_runs_created_at ON workflow_runs (created_at DESC);
