// Package store persists webhook idempotency state and workflow run history
// in Cloudflare D1, per docs/infrastructure/overview-infras.md.
package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/syumai/workers-go/cloudflare/d1" // registers the "d1" driver
)

// Binding is the D1 binding name declared in wrangler.toml.
const Binding = "DB"

type Store struct {
	db *sql.DB
}

func Open() (*Store, error) {
	db, err := sql.Open("d1", Binding)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// RecordDelivery inserts a pending delivery row for deliveryID.
// isNew is false when the row already existed, meaning GitHub redelivered a
// webhook this worker already accepted; callers should skip further
// processing in that case instead of notifying Telegram twice.
func (s *Store) RecordDelivery(ctx context.Context, deliveryID string) (isNew bool, err error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO deliveries (delivery_id, status, attempts, created_at, updated_at)
		VALUES (?, 'pending', 0, ?, ?)
		ON CONFLICT (delivery_id) DO NOTHING
	`, deliveryID, now, now)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteDelivery removes a delivery row. It is used to roll back
// RecordDelivery when the worker fails to enqueue the event for processing,
// so a subsequent GitHub retry of the same delivery isn't skipped forever.
func (s *Store) DeleteDelivery(ctx context.Context, deliveryID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deliveries WHERE delivery_id = ?`, deliveryID)
	return err
}

func (s *Store) MarkDelivered(ctx context.Context, deliveryID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE deliveries SET status = 'delivered', updated_at = ? WHERE delivery_id = ?
	`, time.Now().Unix(), deliveryID)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, deliveryID string, lastErr string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE deliveries
		SET status = 'failed', attempts = attempts + 1, last_error = ?, updated_at = ?
		WHERE delivery_id = ?
	`, lastErr, time.Now().Unix(), deliveryID)
	return err
}

type WorkflowRunRecord struct {
	DeliveryID   string
	Repository   string
	WorkflowName string
	RunID        int64
	RunNumber    int64
	Status       string
	Conclusion   string
	HeadBranch   string
	HeadSHA      string
	Actor        string
	HTMLURL      string
}

func (s *Store) SaveWorkflowRun(ctx context.Context, r WorkflowRunRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_runs (
			delivery_id, repository, workflow_name, run_id, run_number,
			status, conclusion, head_branch, head_sha, actor, html_url, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.DeliveryID, r.Repository, r.WorkflowName, r.RunID, r.RunNumber,
		r.Status, r.Conclusion, r.HeadBranch, r.HeadSHA, r.Actor, r.HTMLURL,
		time.Now().Unix(),
	)
	return err
}
