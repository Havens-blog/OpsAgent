// Package dingtalk provides DingTalk bot integration for incident notifications.
package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// Bot represents a DingTalk bot instance.
type Bot struct {
	WebhookURL string
	secret     string
	timeout    time.Duration
	client     *http.Client
}

// BotOption configures a Bot.
type BotOption func(*Bot)

// WithSecret sets the bot secret for message signing.
func WithSecret(secret string) BotOption {
	return func(b *Bot) {
		b.secret = secret
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(timeout time.Duration) BotOption {
	return func(b *Bot) {
		b.timeout = timeout
	}
}

// NewBot creates a new DingTalk bot.
func NewBot(webhookURL string, opts ...BotOption) *Bot {
	bot := &Bot{
		WebhookURL: webhookURL,
		timeout:    10 * time.Second,
		client:     &http.Client{Timeout: 10 * time.Second},
	}

	for _, opt := range opts {
		opt(bot)
	}

	if bot.client.Timeout == 0 || bot.client.Timeout != bot.timeout {
		bot.client.Timeout = bot.timeout
	}

	return bot
}

// Message represents a DingTalk message.
type Message interface {
	Type() string
	Content() map[string]interface{}
}

// TextMessage represents a text message.
type TextMessage struct {
	Text string `json:"content"`
	At   *At    `json:"at,omitempty"`
}

// At represents @ mentions.
type At struct {
	AtMobiles []string `json:"atMobiles,omitempty"`
	AtUserIDs []string `json:"atUserIds,omitempty"`
	IsAtAll   bool     `json:"isAtAll,omitempty"`
}

// Type implements Message.
func (m *TextMessage) Type() string {
	return "text"
}

// Content implements Message.
func (m *TextMessage) Content() map[string]interface{} {
	return map[string]interface{}{
		"content": m.Text,
		"at":      m.At,
	}
}

// MarkdownMessage represents a markdown message.
type MarkdownMessage struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	At    *At    `json:"at,omitempty"`
}

// Type implements Message.
func (m *MarkdownMessage) Type() string {
	return "markdown"
}

// Content implements Message.
func (m *MarkdownMessage) Content() map[string]interface{} {
	return map[string]interface{}{
		"title": m.Title,
		"text":  m.Text,
		"at":    m.At,
	}
}

// LinkMessage represents a link message.
type LinkMessage struct {
	Title      string `json:"title"`
	Text       string `json:"text"`
	MessageURL string `json:"messageUrl"`
	PicURL     string `json:"picUrl,omitempty"`
}

// Type implements Message.
func (m *LinkMessage) Type() string {
	return "link"
}

// Content implements Message.
func (m *LinkMessage) Content() map[string]interface{} {
	return map[string]interface{}{
		"title":      m.Title,
		"text":       m.Text,
		"messageUrl": m.MessageURL,
		"picUrl":     m.PicURL,
	}
}

// FeedCardMessage represents a feed card message.
type FeedCardMessage struct {
	Links []FeedCardLink `json:"links"`
}

type FeedCardLink struct {
	Title      string `json:"title"`
	MessageURL string `json:"messageUrl"`
	PicURL     string `json:"picUrl"`
}

// Type implements Message.
func (m *FeedCardMessage) Type() string {
	return "feedCard"
}

// Content implements Message.
func (m *FeedCardMessage) Content() map[string]interface{} {
	return map[string]interface{}{
		"links": m.Links,
	}
}

// SendMessage sends a message to DingTalk.
func (b *Bot) SendMessage(ctx context.Context, msg Message) error {
	// Build request body
	body := map[string]interface{}{
		"msgtype": msg.Type(),
	}

	// Add message content based on type
	switch msg.Type() {
	case "text":
		body["text"] = msg.Content()
	case "markdown":
		body["markdown"] = msg.Content()
	case "link":
		body["link"] = msg.Content()
	case "feedCard":
		body["feedCard"] = msg.Content()
	default:
		return fmt.Errorf("unsupported message type: %s", msg.Type())
	}

	// Marshal body
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Build URL with signature if secret is configured
	reqURL := b.WebhookURL
	if b.secret != "" {
		timestamp := time.Now().UnixMilli()
		signature := b.signMessage(timestamp, string(jsonBody))
		reqURL = fmt.Sprintf("%s&timestamp=%d&sign=%s", reqURL, timestamp, url.QueryEscape(signature))
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if errcode, ok := result["errcode"].(float64); ok && errcode != 0 {
		errmsg, _ := result["errmsg"].(string)
		return fmt.Errorf("DingTalk API error: %s (code: %.0f)", errmsg, errcode)
	}

	return nil
}

// signMessage signs a message using HMAC-SHA256.
func (b *Bot) signMessage(timestamp int64, message string) string {
	if b.secret == "" {
		return ""
	}

	// Create signature string: timestamp + "\n" + secret
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, b.secret)

	// Create HMAC-SHA256
	h := hmac.New(sha256.New, []byte(b.secret))
	h.Write([]byte(stringToSign))

	// Base64 encode
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// SendIncidentAlert sends an incident alert to DingTalk.
func (b *Bot) SendIncidentAlert(ctx context.Context, incident *models.Incident, environment, rootCause string) error {
	message := b.formatIncidentMessage(incident, environment, rootCause)

	msg := &MarkdownMessage{
		Title: fmt.Sprintf("🚨 Incident Alert: %s", incident.ID),
		Text:  message,
	}

	return b.SendMessage(ctx, msg)
}

// SendRCACompletedAlert sends an RCA completion alert to DingTalk.
func (b *Bot) SendRCACompletedAlert(ctx context.Context, report *models.RCAReport) error {
	message := b.formatRCAMessage(report)

	msg := &MarkdownMessage{
		Title: fmt.Sprintf("✅ RCA Completed: %s", report.Incident.ID),
		Text:  message,
	}

	return b.SendMessage(ctx, msg)
}

// formatIncidentMessage formats an incident as a markdown message.
func (b *Bot) formatIncidentMessage(incident *models.Incident, environment, rootCause string) string {
	var sb strings.Builder

	// Severity emoji
	severityEmoji := "🟡"
	switch incident.Severity {
	case "P0", "P1":
		severityEmoji = "🔴"
	case "P2":
		severityEmoji = "🟠"
	case "P3":
		severityEmoji = "🟡"
	case "P4":
		severityEmoji = "🟢"
	}

	sb.WriteString(fmt.Sprintf("### %s %s\n\n", severityEmoji, incident.ID))
	sb.WriteString(fmt.Sprintf("**Environment:** %s\n\n", environment))
	sb.WriteString(fmt.Sprintf("**Severity:** %s\n\n", incident.Severity))

	if incident.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", incident.Description))
	}

	if len(incident.AffectedServices) > 0 {
		sb.WriteString("**Affected Services:**\n")
		for _, service := range incident.AffectedServices {
			sb.WriteString(fmt.Sprintf("- %s\n", service))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("**Start Time:** %s\n", incident.StartTime.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("**End Time:** %s\n\n", incident.EndTime.Format("2006-01-02 15:04:05")))

	if rootCause != "" {
		sb.WriteString(fmt.Sprintf("**Potential Root Cause:** %s\n\n", rootCause))
	}

	sb.WriteString("---\n\n")
	sb.WriteString("*Investigation in progress...*")

	return sb.String()
}

// formatRCAMessage formats an RCA report as a markdown message.
func (b *Bot) formatRCAMessage(report *models.RCAReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## RCA Report: %s\n\n", report.Incident.ID))

	// Confidence
	confidenceEmoji := "✅"
	if report.Confidence < 0.5 {
		confidenceEmoji = "⚠️"
	} else if report.Confidence < 0.7 {
		confidenceEmoji = "🟡"
	}
	sb.WriteString(fmt.Sprintf("**Confidence:** %s %.1f%%\n\n", confidenceEmoji, report.Confidence*100))

	// Root cause
	if report.TopCandidate != nil {
		sb.WriteString("### 🔍 Root Cause\n\n")
		sb.WriteString(fmt.Sprintf("**Service:** %s\n\n", report.TopCandidate.Hypothesis.Service))
		sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", report.TopCandidate.Hypothesis.Description))
		sb.WriteString(fmt.Sprintf("**Score:** %.2f\n\n", report.TopCandidate.Score))

		if len(report.TopCandidate.Hypothesis.Evidence) > 0 {
			sb.WriteString("**Evidence:**\n")
			for _, evidence := range report.TopCandidate.Hypothesis.Evidence {
				sb.WriteString(fmt.Sprintf("- %s\n", evidence))
			}
			sb.WriteString("\n")
		}
	}

	// Additional candidates
	if len(report.Candidates) > 1 {
		sb.WriteString("### 📋 Other Candidates\n\n")
		for i, candidate := range report.Candidates {
			if i == 0 || candidate.Hypothesis.Service == report.TopCandidate.Hypothesis.Service {
				continue
			}
			sb.WriteString(fmt.Sprintf("**%d. %s** (Score: %.2f)\n", i, candidate.Hypothesis.Service, candidate.Score))
			sb.WriteString(fmt.Sprintf("   %s\n\n", candidate.Hypothesis.Description))
		}
	}

	// Summary
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("*Analysis completed at %s*", time.Now().Format("2006-01-02 15:04:05")))

	return sb.String()
}

// SendBatchMessages sends multiple messages in batch.
func (b *Bot) SendBatchMessages(ctx context.Context, messages []Message) error {
	for _, msg := range messages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := b.SendMessage(ctx, msg); err != nil {
				return fmt.Errorf("failed to send message: %w", err)
			}
		}
	}
	return nil
}
