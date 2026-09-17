package main

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/queues"

	ghwebhook "github.com/v2d27/github-workflow-telegram-notification/internal/github"
	"github.com/v2d27/github-workflow-telegram-notification/internal/notifier"
	"github.com/v2d27/github-workflow-telegram-notification/internal/store"
	"github.com/v2d27/github-workflow-telegram-notification/internal/telegram"
)

// defaultAvatarSize is used when the AVATAR_SIZE var (see wrangler.toml
// [vars]) is unset or invalid.
const defaultAvatarSize = 128

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

	avatarSize := defaultAvatarSize
	if raw := cloudflare.Getenv("AVATAR_SIZE"); raw != "" {
		if size, err := strconv.Atoi(raw); err == nil && size > 0 {
			avatarSize = size
		} else {
			log.Printf("main: ignoring invalid AVATAR_SIZE %q, using default %d", raw, defaultAvatarSize)
		}
	}

	consumer := notifier.NewConsumer(st, tg, gh, avatarSize)
	queues.ConsumeNonBlock(func(batch *queues.MessageBatch) error {
		return consumer.HandleBatch(context.Background(), batch)
	})

	webhookHandler := ghwebhook.NewHandler(cloudflare.Getenv("WEBHOOK_SECRET_GITHUB"), st)
	http.Handle("/webhooks/github", webhookHandler)

	workers.Serve(nil) // use http.DefaultServeMux
}
