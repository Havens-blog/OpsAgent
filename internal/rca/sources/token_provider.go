package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenProvider 换取 e-cam 服务间访问令牌（S2S）。
type TokenProvider interface {
	// TokenFor 返回缓存中的有效令牌，或换取新令牌。失败返回 error。
	TokenFor(ctx context.Context) (string, error)
}

// tokenResp eiam OAuth2 token 端点响应（仅取所需字段）。
type tokenResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// ServiceTokenProvider 用 client_id/secret 向 eiam 换取 client_credentials
// 模式的 access token（携带固定租户上下文），缓存 + 提前刷新。
type ServiceTokenProvider struct {
	tokenURL     string
	clientID     string
	clientSecret string
	tenantID     int64
	client       *http.Client

	mu     sync.Mutex
	cached string
	expiry time.Time
}

// NewServiceTokenProvider 构造令牌换取器。参数留空不报错，换取期 fail-closed。
func NewServiceTokenProvider(tokenURL, clientID, clientSecret string, tenantID int64) *ServiceTokenProvider {
	return &ServiceTokenProvider{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		tenantID:     tenantID,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// TokenFor 返回缓存中的有效令牌，或换取新令牌并缓存（提前 60s 刷新）。
func (p *ServiceTokenProvider) TokenFor(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != "" && time.Now().Before(p.expiry) {
		return p.cached, nil
	}

	token, ttl, err := p.exchange(ctx)
	if err != nil {
		return "", err
	}

	expiry := time.Now().Add(ttl - 60*time.Second)
	if !expiry.After(time.Now()) {
		expiry = time.Now().Add(ttl / 2)
	}
	p.cached = token
	p.expiry = expiry
	return token, nil
}

func (p *ServiceTokenProvider) exchange(ctx context.Context) (string, time.Duration, error) {
	if p.tokenURL == "" {
		return "", 0, fmt.Errorf("eiam token url 未配置")
	}
	if p.clientID == "" || p.clientSecret == "" {
		return "", 0, fmt.Errorf("eiam client_id/client_secret 未配置")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.clientSecret)
	form.Set("tenant_id", strconv.FormatInt(p.tenantID, 10))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("构造 token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("调用 eiam token 端点失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("eiam token 端点返回非 200: %d", resp.StatusCode)
	}

	var body tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", 0, fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if body.AccessToken == "" {
		return "", 0, fmt.Errorf("token 响应缺少 access_token")
	}

	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return body.AccessToken, ttl, nil
}
