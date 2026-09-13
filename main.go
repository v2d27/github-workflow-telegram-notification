package main

import (
	"context"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/queues"

	ghwebhook "github.com/v2d27/github-workflow-telegram-notification/internal/github"
	"github.com/v2d27/github-workflow-telegram-notification/internal/notifier"
	"github.com/v2d27/github-workflow-telegram-notification/internal/store"
	"github.com/v2d27/github-workflow-telegram-notification/internal/telegram"
)

func main() {
	st, err := store.Open()
	if err != nil {
		log.Fatalf("failed to open D1 store: %v", err)
	}

	tg := telegram.NewClient(
		cloudflare.Getenv("TELEGRAM_BOT_TOKEN"),
		cloudflare.Getenv("TELEGRAM_CHAT_ID"),
	)

	gh := ghwebhook.NewAppClient(
		cloudflare.Getenv("GITHUB_APP_ID"),
		cloudflare.Getenv("GITHUB_APP_INSTALLATION_ID"),
		cloudflare.Getenv("GITHUB_APP_PRIVATE_KEY"),
	)

	consumer := notifier.NewConsumer(st, tg, gh)
	queues.ConsumeNonBlock(func(batch *queues.MessageBatch) error {
		return consumer.HandleBatch(context.Background(), batch)
	})

	webhookHandler := ghwebhook.NewHandler(cloudflare.Getenv("WEBHOOK_SECRET_GITHUB"), st)
	http.Handle("/webhooks/github", webhookHandler)

	workers.Serve(nil) // use http.DefaultServeMux
}
