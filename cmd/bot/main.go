// Package main is the entry point for the OpsAgent DingTalk bot service.
// It initializes the bot for outbound incident notifications. In production
// it is wired to alertmanager / RCA completion events to dispatch messages.
package main

import (
	"fmt"
	"os"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/dingtalk"
)

func main() {
	settings := config.Get()

	webhookURL := settings.DingTalk.WebhookURL
	if webhookURL == "" {
		fmt.Fprintln(os.Stderr, "DINGTALK_WEBHOOK_URL is not configured.")
		fmt.Fprintln(os.Stderr, "Hint: set DINGTALK_WEBHOOK_URL (and optionally DINGTALK_SECRET) in the environment or config.")
		os.Exit(1)
	}

	bot := dingtalk.NewBot(
		webhookURL,
		dingtalk.WithSecret(settings.DingTalk.Secret),
	)

	fmt.Printf("OpsAgent DingTalk bot initialized (webhook configured).\n")
	_ = bot
	fmt.Println("Wire the bot to alertmanager notifications to dispatch incident/RCA messages.")
}
