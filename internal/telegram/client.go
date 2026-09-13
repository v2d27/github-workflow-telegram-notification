// Package telegram sends workflow run notifications to a Telegram chat via
// the Bot API, per docs/infrastructure/overview-infras.md.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

const apiBase = "https://api.telegram.org"

type Client struct {
	botToken string
	chatID   string
	http     *fetch.Client
}

func NewClient(botToken, chatID string) *Client {
	return &Client{botToken: botToken, chatID: chatID, http: fetch.NewClient()}
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// SendMessage posts text (HTML-formatted, see format.go) to the configured
// chat. A non-2xx response or transport error is returned as-is so the
// caller can retry the message via the queue.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	payload, err := json.Marshal(sendMessageRequest{
		ChatID:                c.chatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", apiBase, c.botToken)
	req, err := fetch.NewRequest(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req, nil)
	if err != nil {
		return fmt.Errorf("telegram: send request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("telegram: unexpected status %d: %s", res.StatusCode, string(body))
	}
	return nil
}
