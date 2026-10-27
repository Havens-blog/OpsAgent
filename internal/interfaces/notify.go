// IM webhook delivery — sends a real test message to the configured
// DingTalk / WeCom / Feishu custom-bot webhook. Signing follows each
// platform's documented algorithm:
//   - DingTalk: sign = urlencode(base64(hmac_sha256(key=secret, msg="{ms_ts}\n{secret}"))),
//     appended to the URL as &timestamp={ms}&sign={sign}
//   - Feishu:   sign = base64(hmac_sha256(key="{s_ts}\n{secret}", msg="")), body
//     carries timestamp={s} + sign
//   - WeCom:    no signing
package interfaces

import (
	"bytes"
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
)

const notifyTestText = "息壤 OpsAgent 测试消息 — IM 通知通道验证成功"

// notificationTestRequest lets the console test the form's current values
// before saving. Empty fields fall back to the stored config.
type notificationTestRequest struct {
	Platform   string `json:"platform"`
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

// testNotificationHandler delivers a real test message. POST only; the LLM
// test handler precedent applies (no admin write gate — it mutates nothing).
func (s *APIServer) testNotificationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed", "")
		return
	}

	req := notificationTestRequest{}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to read request", err.Error())
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
				return
			}
		}
	}

	// Fill gaps from the stored config (test-before-save UX: the form sends
	// its current values; an untouched field keeps the stored one).
	stored := s.settings.ToNotificationConfig()
	platform := req.Platform
	if platform == "" {
		platform = stored.Platform
	}
	webhook := req.WebhookURL
	if webhook == "" {
		webhook = stored.WebhookURL
	}
	secret := req.Secret
	if secret == "" {
		secret = stored.Secret
	}

	if webhook == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_webhook",
			"未配置 Webhook URL,请先填写或保存通知配置", "")
		return
	}
	if platform == "" {
		platform = "dingtalk"
	}

	start := time.Now()
	status, respBody, err := deliverIMMessage(platform, webhook, secret, notifyTestText)
	latency := time.Since(start)

	s.recordConfigChange(r, "发送IM测试消息", err)

	if err != nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"platform":   platform,
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"success":       imReplyOK(platform, respBody),
		"platform":      platform,
		"http_status":   status,
		"response":      respBody,
		"latency_ms":    latency.Milliseconds(),
	})
}

// deliverIMMessage posts the platform-specific payload and returns the HTTP
// status plus raw response body. A network/transport failure surfaces as err;
// an HTTP-level reply (even 4xx) is returned for platform-code inspection.
func deliverIMMessage(platform, webhook, secret, text string) (int, string, error) {
	var payload map[string]interface{}
	finalURL := webhook

	switch platform {
	case "dingtalk":
		payload = map[string]interface{}{
			"msgtype": "text",
			"text":    map[string]string{"content": text},
		}
		if secret != "" {
			ts := time.Now().UnixMilli()
			sign := dingTalkSign(secret, ts)
			sep := "&"
			if !strings.Contains(webhook, "?") {
				sep = "?"
			}
			finalURL = fmt.Sprintf("%s%stimestamp=%d&sign=%s", webhook, sep, ts, sign)
		}
	case "wecom":
		payload = map[string]interface{}{
			"msgtype": "text",
			"text":    map[string]string{"content": text},
		}
	case "feishu":
		payload = map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]string{"text": text},
		}
		if secret != "" {
			ts := time.Now().Unix()
			payload["timestamp"] = fmt.Sprintf("%d", ts)
			payload["sign"] = feishuSign(secret, ts)
		}
	default:
		return 0, "", fmt.Errorf("不支持的平台: %s (dingtalk/wecom/feishu)", platform)
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		return 0, "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(finalURL, "application/json", bytes.NewReader(buf))
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}

// imReplyOK inspects the platform reply for its success code:
// DingTalk/WeCom {"errcode":0}, Feishu {"code":0}. A 2xx reply without a
// recognizable code field is treated as delivered (permissive for proxies).
func imReplyOK(platform, body string) bool {
	var m map[string]interface{}
	if json.Unmarshal([]byte(body), &m) != nil {
		return false
	}
	if v, ok := m["errcode"].(float64); ok {
		return v == 0
	}
	if v, ok := m["code"].(float64); ok {
		return v == 0
	}
	return false
}

// dingTalkSign implements the DingTalk custom-bot signature.
func dingTalkSign(secret string, timestampMS int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestampMS, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// feishuSign implements the Feishu custom-bot signature: the HMAC key IS the
// "{timestamp}\n{secret}" string and the message is empty.
func feishuSign(secret string, timestampS int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestampS, secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	mac.Write([]byte(""))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
