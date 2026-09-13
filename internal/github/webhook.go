package github

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/v2d27/github-workflow-telegram-notification/internal/queue"
	"github.com/v2d27/github-workflow-telegram-notification/internal/store"
)

// Handler receives the GitHub App's workflow_run webhook, verifies it,
// deduplicates it against D1, and hands it off to the Cloudflare Queue for
// asynchronous Telegram delivery. It intentionally never calls back into the
// GitHub API: the workflow_run payload already carries everything needed to
// report success/failure.
type Handler struct {
	webhookSecret string
	store         *store.Store
}

func NewHandler(webhookSecret string, st *store.Store) *Handler {
	return &Handler{webhookSecret: webhookSecret, store: st}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer req.Body.Close()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !VerifySignature(h.webhookSecret, body, req.Header.Get("X-Hub-Signature-256")) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Events this worker doesn't act on (e.g. the App's initial "ping", or
	// any event type other than workflow_run) are acknowledged with 200 so
	// GitHub doesn't retry them.
	if req.Header.Get("X-GitHub-Event") != "workflow_run" {
		w.WriteHeader(http.StatusOK)
		return
	}

	deliveryID := req.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var event WorkflowRunEvent
	if err := json.Unmarshal(body, &event); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Only "completed" runs carry a final conclusion worth notifying about;
	// "requested"/"in_progress" updates are ignored.
	if event.Action != "completed" {
		w.WriteHeader(http.StatusOK)
		return
	}

	isNew, err := h.store.RecordDelivery(req.Context(), deliveryID)
	if err != nil {
		log.Printf("webhook: failed to record delivery %s: %v", deliveryID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !isNew {
		// Duplicate delivery: GitHub redelivered a webhook already accepted.
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := queue.Enqueue(deliveryID, body); err != nil {
		log.Printf("webhook: failed to enqueue delivery %s: %v", deliveryID, err)
		// Roll back so a GitHub retry of this same delivery isn't silently
		// swallowed as a false "duplicate" next time.
		if delErr := h.store.DeleteDelivery(req.Context(), deliveryID); delErr != nil {
			log.Printf("webhook: failed to roll back delivery %s: %v", deliveryID, delErr)
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
