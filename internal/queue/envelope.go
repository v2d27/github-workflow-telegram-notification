// Package queue wraps the Cloudflare Queue that decouples the webhook
// receiver from the Telegram notifier, per
// docs/infrastructure/overview-infras.md.
package queue

import (
	"encoding/json"

	"github.com/syumai/workers-go/cloudflare/queues"
)

// Binding is the queue producer binding name declared in wrangler.toml.
const Binding = "QUEUE"

// Envelope is the JSON message shape sent through the queue. GitHub's
// delivery ID only exists in the X-GitHub-Delivery request header, not in
// the webhook body, so it must be carried alongside the raw event for the
// consumer to correlate the message back to its deliveries row.
type Envelope struct {
	DeliveryID string          `json:"delivery_id"`
	Event      json.RawMessage `json:"event"`
}

// Enqueue sends a workflow_run event to the queue for asynchronous delivery
// to Telegram.
func Enqueue(deliveryID string, rawEvent []byte) error {
	payload, err := json.Marshal(Envelope{DeliveryID: deliveryID, Event: rawEvent})
	if err != nil {
		return err
	}

	p, err := queues.NewProducer(Binding)
	if err != nil {
		return err
	}
	return p.SendText(string(payload))
}
