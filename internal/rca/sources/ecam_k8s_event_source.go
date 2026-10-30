package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// ECamK8sEventSource consumes Kubernetes events from the e-cam log federation
// (log_type "k8s-event": ACK cluster eventer → SLS k8s-event stores) and maps
// them onto models.Change with Type "k8s_event". The RCA engine turns the
// resource-failure subset (OOMKilled / Unhealthy / ...) into
// HypothesisTypeResource — a distinct root-cause signal from deployment/config
// changes.
//
// It reuses the same S2S token plumbing as ECamLogSource (client_credentials
// → e-cam /api/s2s/logs), so no cloud AK/SK is needed here.
type ECamK8sEventSource struct {
	endpoint      string
	token         string
	tokenProvider TokenProvider
	pathPrefix    string
	client        *http.Client
}

// NewECamK8sEventSource builds a k8s-event change source over the e-cam
// federation. timeout <= 0 defaults to 30s.
func NewECamK8sEventSource(endpoint, token string, timeout time.Duration) *ECamK8sEventSource {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ECamK8sEventSource{
		endpoint:   strings.TrimRight(endpoint, "/"),
		token:      token,
		pathPrefix: "/api/v1/cam/logs",
		client:     &http.Client{Timeout: timeout},
	}
}

// WithTokenProvider injects the S2S token exchanger and switches the path to
// the /api/s2s/logs group (path + auth must move together to avoid a 401).
func (s *ECamK8sEventSource) WithTokenProvider(p TokenProvider) *ECamK8sEventSource {
	s.tokenProvider = p
	s.pathPrefix = "/api/s2s/logs"
	return s
}

func (s *ECamK8sEventSource) bearerToken(ctx context.Context) (string, error) {
	if s.tokenProvider != nil {
		return s.tokenProvider.TokenFor(ctx)
	}
	return s.token, nil
}

// ecamK8sEventEntry mirrors the federation k8s-event entry shape (subset).
type ecamK8sEventEntry struct {
	Timestamp int64  `json:"timestamp"`
	ClusterID string `json:"cluster_id"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Object    string `json:"object"`
	Reason    string `json:"reason"`
	Level     string `json:"level"`
	Count     int64  `json:"count"`
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
	Message   string `json:"message"`
}

type ecamK8sEventSearchData struct {
	Total     int64               `json:"total"`
	Entries   []ecamK8sEventEntry `json:"entries"`
	Truncated bool                `json:"truncated"`
}

// k8sEventQueryLimit caps events fetched per RCA query (same bound as logs).
const k8sEventQueryLimit = 300

// QueryChanges returns K8s events in the window mapped onto models.Change.
func (s *ECamK8sEventSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	return s.search(ctx, ecamSearchRequest{
		LogType:   "k8s-event",
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Limit:     k8sEventQueryLimit,
	})
}

// QueryChangesByService returns K8s events whose involved object matches service.
func (s *ECamK8sEventSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("ecam k8s event source: empty service name")
	}
	return s.search(ctx, ecamSearchRequest{
		LogType:   "k8s-event",
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Filters:   []ecamFieldFilter{{Field: "object", Op: "eq", Value: service}},
		Limit:     k8sEventQueryLimit,
	})
}

// GetLastChange returns the most recent event for a service (latest-first).
func (s *ECamK8sEventSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	changes, err := s.QueryChangesByService(ctx, service, time.Now().Add(-24*time.Hour), time.Now())
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return &changes[0], nil
}

// Ping probes the federation for reachability (pinger interface).
func (s *ECamK8sEventSource) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+s.pathPrefix+"/types", nil)
	if err != nil {
		return fmt.Errorf("ecam k8s event source: build request: %w", err)
	}
	tok, err := s.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("ecam k8s event source: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ecam k8s event source: ping %s: %w", s.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ecam k8s event source: ping status %d", resp.StatusCode)
	}
	var env ecamResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("ecam k8s event source: ping decode: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("ecam k8s event source: ping code %d: %s", env.Code, env.Message)
	}
	return nil
}

// search posts a k8s-event search and maps entries into models.Change.
func (s *ECamK8sEventSource) search(ctx context.Context, body ecamSearchRequest) ([]models.Change, error) {
	var data ecamK8sEventSearchData
	if err := s.post(ctx, s.pathPrefix+"/search", body, &data); err != nil {
		return nil, err
	}
	if data.Truncated {
		log.Printf("[ecam-k8s-event-source] WARN: search truncated at the federation merge cap (total=%d, returned=%d); resource counts may under-report", data.Total, len(data.Entries))
	}
	out := make([]models.Change, 0, len(data.Entries))
	for _, e := range data.Entries {
		out = append(out, s.mapChange(e))
	}
	return out, nil
}

// post performs one authenticated federation call and decodes data into out.
func (s *ECamK8sEventSource) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ecam k8s event source: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("ecam k8s event source: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	tok, err := s.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("ecam k8s event source: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ecam k8s event source: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("ecam k8s event source: POST %s status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var env ecamResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("ecam k8s event source: POST %s decode: %w", path, err)
	}
	if env.Code != 0 {
		return fmt.Errorf("ecam k8s event source: POST %s code %d: %s", path, env.Code, env.Message)
	}
	if len(env.Data) == 0 {
		return nil // honest empty
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("ecam k8s event source: POST %s data decode: %w", path, err)
	}
	return nil
}

// mapChange converts one k8s-event entry into a change with Type "k8s_event".
func (s *ECamK8sEventSource) mapChange(e ecamK8sEventEntry) models.Change {
	ts := e.LastSeen
	if ts == 0 {
		ts = e.Timestamp
	}
	cluster := e.Cluster
	if cluster == "" {
		cluster = e.ClusterID
	}
	service := e.Object
	if service == "" {
		service = e.Namespace + "/" + e.Kind
	}
	id := strings.Join([]string{cluster, e.Namespace, e.Kind, e.Object, e.Reason}, "/")
	return models.Change{
		ID:          id,
		Timestamp:   time.UnixMilli(ts),
		Service:     service,
		Type:        "k8s_event",
		Description: fmt.Sprintf("%s %s/%s: %s", e.Reason, e.Namespace, e.Object, firstLine(e.Message)),
		Details: map[string]string{
			"reason":     e.Reason,
			"kind":       e.Kind,
			"namespace":  e.Namespace,
			"object":     e.Object,
			"cluster":    cluster,
			"level":      e.Level,
			"message":    e.Message,
			"first_seen": strconv.FormatInt(e.FirstSeen, 10),
			"count":      strconv.FormatInt(e.Count, 10),
		},
	}
}

// firstLine truncates a message to its first line (bounded), so an event
// description never balloons from a multi-line K8s message.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
