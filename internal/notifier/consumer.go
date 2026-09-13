// Package notifier is the Cloudflare Queue consumer: it turns a queued
// workflow_run event into a Telegram message and records the outcome in D1,
// per docs/infrastructure/overview-infras.md.
package notifier

import (
	"context"
	"encoding/json"
	"log"

	"github.com/syumai/workers-go/cloudflare/queues"

	ghevents "github.com/v2d27/github-workflow-telegram-notification/internal/github"
	"github.com/v2d27/github-workflow-telegram-notification/internal/queue"
	"github.com/v2d27/github-workflow-telegram-notification/internal/store"
	"github.com/v2d27/github-workflow-telegram-notification/internal/telegram"
)

type Consumer struct {
	store    *store.Store
	telegram *telegram.Client
	github   *ghevents.AppClient
}

func NewConsumer(st *store.Store, tg *telegram.Client, gh *ghevents.AppClient) *Consumer {
	return &Consumer{store: st, telegram: tg, github: gh}
}

// HandleBatch processes every message individually: each message is acked
// or explicitly marked for retry, so the batch handler itself always
// returns nil (Cloudflare Queues only redelivers messages that were neither
// acked nor retried).
func (c *Consumer) HandleBatch(ctx context.Context, batch *queues.MessageBatch) error {
	for _, msg := range batch.Messages {
		if err := c.handleMessage(ctx, msg); err != nil {
			log.Printf("notifier: failed to process message %s: %v", msg.ID, err)
			msg.Retry()
			continue
		}
		msg.Ack()
	}
	return nil
}

func (c *Consumer) handleMessage(ctx context.Context, msg *queues.Message) error {
	body, err := msg.StringBody()
	if err != nil {
		return err
	}

	var envelope queue.Envelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return err
	}

	var event ghevents.WorkflowRunEvent
	if err := json.Unmarshal(envelope.Event, &event); err != nil {
		return err
	}

	if err := c.store.SaveWorkflowRun(ctx, store.WorkflowRunRecord{
		DeliveryID:   envelope.DeliveryID,
		Repository:   event.Repository.FullName,
		WorkflowName: event.WorkflowRun.Name,
		RunID:        event.WorkflowRun.ID,
		RunNumber:    event.WorkflowRun.RunNumber,
		Status:       event.WorkflowRun.Status,
		Conclusion:   event.WorkflowRun.Conclusion,
		HeadBranch:   event.WorkflowRun.HeadBranch,
		HeadSHA:      event.WorkflowRun.HeadSHA,
		Actor:        event.WorkflowRun.Actor.Login,
		HTMLURL:      event.WorkflowRun.HTMLURL,
	}); err != nil {
		return err
	}

	// Enrichment (failed jobs/steps, commit author, PR requester/merger) is
	// best-effort: a GitHub API hiccup shouldn't block or endlessly retry a
	// notification that would otherwise be ready to send.
	includeJobs := event.WorkflowRun.Conclusion != "success"
	details, err := c.github.FetchRunDetails(ctx, event.Repository.FullName, event.WorkflowRun.ID, event.WorkflowRun.HeadSHA, includeJobs)
	if err != nil {
		log.Printf("notifier: failed to enrich delivery %s from GitHub API: %v", envelope.DeliveryID, err)
		details = nil
	}

	text := telegram.FormatWorkflowRun(event.Repository.FullName, event.WorkflowRun, event.Sender, details)
	if err := c.telegram.SendMessage(ctx, text); err != nil {
		if markErr := c.store.MarkFailed(ctx, envelope.DeliveryID, err.Error()); markErr != nil {
			log.Printf("notifier: failed to mark delivery %s failed: %v", envelope.DeliveryID, markErr)
		}
		return err
	}

	return c.store.MarkDelivered(ctx, envelope.DeliveryID)
}
