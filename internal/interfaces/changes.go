package interfaces

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// changeItemDTO is the wire format for one real change record returned by
// /api/v1/changes. It is a faithful projection of models.Change — no invented
// fields (no fabricated author/status/correlation). The namespace/reason/
// level/object/object_kind/cluster fields are lifted out of Details for
// filtering and rendering; Details is still passed through untouched.
//
// occurrences/first_seen/last_seen are always emitted: they are exact
// (1 / the record's own timestamp) for an un-deduplicated record and the
// group size and true extremes when the caller asked for dedupe=true.
type changeItemDTO struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	System      string            `json:"system"`
	Service     string            `json:"service"`
	Message     string            `json:"message"`
	Time        string            `json:"time"`
	Namespace   string            `json:"namespace,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Level       string            `json:"level,omitempty"`
	Object      string            `json:"object,omitempty"`
	ObjectKind  string            `json:"object_kind,omitempty"`
	Cluster     string            `json:"cluster,omitempty"`
	Occurrences int               `json:"occurrences"`
	FirstSeen   string            `json:"first_seen"`
	LastSeen    string            `json:"last_seen"`
	Details     map[string]string `json:"details,omitempty"`
}

// changeSystem derives the originating-system label for a change record. Both
// real change sources (K8s deployment conditions, e-cam k8s-events) belong to
// the Kubernetes family; an explicit "system" detail wins when a future source
// provides one, otherwise it is honestly reported as "kubernetes".
func changeSystem(c models.Change) string {
	if c.Details != nil {
		if sys := strings.TrimSpace(c.Details["system"]); sys != "" {
			return sys
		}
	}
	return "kubernetes"
}

// changeItemFromModel projects one real models.Change onto the wire DTO.
func changeItemFromModel(c models.Change) changeItemDTO {
	item := changeItemDTO{
		ID:      c.ID,
		Kind:    c.Type,
		System:  changeSystem(c),
		Service: c.Service,
		Message: c.Description,
		Time:    c.Timestamp.Format(time.RFC3339),
		Details: c.Details,
	}
	if c.Details != nil {
		item.Namespace = strings.TrimSpace(c.Details["namespace"])
		item.Reason = strings.TrimSpace(c.Details["reason"])
		item.Level = strings.TrimSpace(c.Details["level"])
		item.Object = strings.TrimSpace(c.Details["object"])
		item.ObjectKind = strings.TrimSpace(c.Details["kind"])
		item.Cluster = strings.TrimSpace(c.Details["cluster"])
	}
	item.Occurrences = 1
	item.FirstSeen = item.Time
	item.LastSeen = item.Time
	return item
}

// changeQuery is the parsed contract of one GET /api/v1/changes request.
type changeQuery struct {
	start     time.Time
	end       time.Time
	order     string
	limit     int
	offset    int
	kind      string
	system    string
	namespace string
	q         string
	dedupe    bool
}

// changeQueryError carries the client-facing reason a query param was
// rejected, so the handler can answer 400 with the exact field at fault.
type changeQueryError struct {
	code    string
	message string
	details string
}

func newChangeQueryError(code, message, details string) *changeQueryError {
	return &changeQueryError{code: code, message: message, details: details}
}

// parseChangeQuery validates the request params. Window: explicit start/end
// (RFC3339) win over hours whenever either is present; hours defaults to 24 and
// is clamped into 1..168. Paging: limit defaults to 20 and is clamped into
// 1..200, offset defaults to 0 and must be a non-negative integer. Malformed
// values are rejected with 400 rather than silently coerced.
func parseChangeQuery(r *http.Request) (changeQuery, *changeQueryError) {
	raw := r.URL.Query()
	q := changeQuery{order: "desc", limit: 20, offset: 0}
	now := time.Now()

	startStr := strings.TrimSpace(raw.Get("start"))
	endStr := strings.TrimSpace(raw.Get("end"))
	if startStr != "" || endStr != "" {
		if startStr != "" {
			start, err := time.Parse(time.RFC3339, startStr)
			if err != nil {
				return q, newChangeQueryError("invalid_start", "start must be RFC3339 (e.g. 2006-01-02T15:04:05Z)", err.Error())
			}
			q.start = start
		} else {
			q.start = now.Add(-24 * time.Hour)
		}
		if endStr != "" {
			end, err := time.Parse(time.RFC3339, endStr)
			if err != nil {
				return q, newChangeQueryError("invalid_end", "end must be RFC3339 (e.g. 2006-01-02T15:04:05Z)", err.Error())
			}
			q.end = end
		} else {
			q.end = now
		}
	} else {
		hours := 24
		if h := strings.TrimSpace(raw.Get("hours")); h != "" {
			parsed, err := strconv.Atoi(h)
			if err != nil {
				return q, newChangeQueryError("invalid_hours", "hours must be an integer between 1 and 168", h)
			}
			hours = parsed
		}
		if hours < 1 {
			hours = 1
		}
		if hours > 168 {
			hours = 168
		}
		q.end = now
		q.start = q.end.Add(-time.Duration(hours) * time.Hour)
	}

	if l := strings.TrimSpace(raw.Get("limit")); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil {
			return q, newChangeQueryError("invalid_limit", "limit must be an integer between 1 and 200", l)
		}
		q.limit = parsed
	}
	if q.limit < 1 {
		q.limit = 1
	}
	if q.limit > 200 {
		q.limit = 200
	}

	if o := strings.TrimSpace(raw.Get("offset")); o != "" {
		parsed, err := strconv.Atoi(o)
		if err != nil || parsed < 0 {
			return q, newChangeQueryError("invalid_offset", "offset must be an integer >= 0", o)
		}
		q.offset = parsed
	}

	if order := strings.ToLower(strings.TrimSpace(raw.Get("order"))); order != "" {
		if order != "desc" && order != "asc" {
			return q, newChangeQueryError("invalid_order", "order must be one of: desc, asc", order)
		}
		q.order = order
	}

	if d := strings.TrimSpace(raw.Get("dedupe")); d != "" {
		parsed, err := strconv.ParseBool(d)
		if err != nil {
			return q, newChangeQueryError("invalid_dedupe", "dedupe must be a boolean (true/false)", d)
		}
		q.dedupe = parsed
	}

	q.kind = strings.TrimSpace(raw.Get("kind"))
	q.system = strings.TrimSpace(raw.Get("system"))
	q.namespace = strings.TrimSpace(raw.Get("namespace"))
	q.q = strings.TrimSpace(raw.Get("q"))
	return q, nil
}

// changesHandler lists REAL change events from the configured change source
// (K8s deployment conditions / e-cam k8s-events).
//
// Query params: explicit start/end (RFC3339, take precedence) or hours
// (default 24, clamped to 1..168); limit (default 20, clamped to 1..200);
// offset (default 0); kind/system/namespace (exact match); q (case-insensitive
// substring over service/object/message/reason); order (desc|asc); dedupe
// (group by object|service + reason, keep the newest, report occurrences).
//
// The expensive source scan is cached for changeCacheTTL as the FULL, already
// ordered window list plus the window-wide stats; every request then filters,
// groups and pages that list in memory.
//
// Hard contract: when no real change source is active (changeSourceReal false,
// i.e. the dimension fell back to the honest empty source), the handler returns
// an empty list with an explanatory hint. It NEVER queries the mock/empty
// fallback and NEVER fabricates records. A real query failure returns 502 with
// the upstream error — never a silent fallback to demo data.
func (s *APIServer) changesHandler(w http.ResponseWriter, r *http.Request) {
	q, qerr := parseChangeQuery(r)
	if qerr != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, qerr.code, qerr.message, qerr.details)
		return
	}

	window := map[string]interface{}{
		"start": q.start.Format(time.RFC3339),
		"end":   q.end.Format(time.RFC3339),
	}

	if !s.changeSourceReal || s.changeSource == nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"changes":   []changeItemDTO{},
			"count":     0,
			"total":     0,
			"offset":    q.offset,
			"limit":     q.limit,
			"has_more":  false,
			"truncated": false,
			"stats":     changeStats(nil),
			"window":    window,
			"source": map[string]interface{}{
				"real": false,
				"type": "",
				"hint": "Real change source not configured (K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE or platform event source)",
			},
		})
		return
	}

	// Quantize both ends so repeated "last N hours" loads share an entry, bind
	// the key to the source instance so a swapped source never serves another
	// source's records, and include the order because the cached list is stored
	// already sorted for that order.
	cacheKey := fmt.Sprintf("%d|%d|%s",
		q.start.Truncate(30*time.Second).Unix(),
		q.end.Truncate(30*time.Second).Unix(),
		q.order)

	items, stats, cached := s.changeCacheGet(cacheKey)
	if !cached {
		changes, err := s.changeSource.QueryChanges(r.Context(), q.start, q.end)
		if err != nil {
			s.writeErrorResponse(w, http.StatusBadGateway, "change_source_error", "Failed to query the real change source", err.Error())
			return
		}

		items = make([]changeItemDTO, 0, len(changes))
		for _, c := range changes {
			items = append(items, changeItemFromModel(c))
		}
		sortChangeItems(items, q.order)
		stats = changeStats(items)
		s.changeCacheSet(cacheKey, items, stats)
	}

	filtered := filterChangeItems(items, q)
	if q.dedupe {
		filtered = dedupeChanges(filtered)
		sortChangeItems(filtered, q.order)
	}

	total := len(filtered)
	offset := q.offset
	if offset > total {
		offset = total
	}
	end := offset + q.limit
	if end > total {
		end = total
	}
	page := make([]changeItemDTO, 0, end-offset)
	page = append(page, filtered[offset:end]...)
	hasMore := end < total

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"changes":   page,
		"count":     len(page),
		"total":     total,
		"offset":    offset,
		"limit":     q.limit,
		"has_more":  hasMore,
		"truncated": hasMore,
		"stats":     stats,
		"source":    map[string]interface{}{"real": true, "type": "kubernetes"},
		"window":    window,
	})
}

// sortChangeItems orders records by their RFC3339 timestamp (RFC3339 strings
// sort chronologically). The record ID breaks ties so equal timestamps keep a
// stable, reproducible order across requests.
func sortChangeItems(items []changeItemDTO, order string) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Time != items[j].Time {
			if order == "asc" {
				return items[i].Time < items[j].Time
			}
			return items[i].Time > items[j].Time
		}
		if order == "asc" {
			return items[i].ID < items[j].ID
		}
		return items[i].ID > items[j].ID
	})
}

// filterChangeItems applies the exact-match kind/system/namespace filters and
// the case-insensitive text search over the real source fields.
func filterChangeItems(items []changeItemDTO, q changeQuery) []changeItemDTO {
	out := make([]changeItemDTO, 0, len(items))
	needle := strings.ToLower(q.q)
	for _, it := range items {
		if q.kind != "" && it.Kind != q.kind {
			continue
		}
		if q.system != "" && it.System != q.system {
			continue
		}
		if q.namespace != "" && it.Namespace != q.namespace {
			continue
		}
		if needle != "" && !changeMatchesQuery(it, needle) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func changeMatchesQuery(it changeItemDTO, needle string) bool {
	for _, field := range []string{it.Service, it.Object, it.Message, it.Reason} {
		if field != "" && strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

// changeGroupKey is the dedupe identity: (object | service, reason).
func changeGroupKey(it changeItemDTO) string {
	subject := strings.TrimSpace(it.Object)
	if subject == "" {
		subject = strings.TrimSpace(it.Service)
	}
	return subject + "\x00" + strings.TrimSpace(it.Reason)
}

// changeGroup accumulates every record sharing one changeGroupKey.
type changeGroup struct {
	newest      changeItemDTO
	occurrences int
	firstSeen   string
	lastSeen    string
}

// dedupeChanges collapses (object|service, reason) groups to their newest
// record, carrying the true group size and the earliest/latest timestamps. The
// result is re-sorted by the caller because the retained record is the newest
// one regardless of the requested order.
func dedupeChanges(items []changeItemDTO) []changeItemDTO {
	groups := make(map[string]*changeGroup, len(items))
	keys := make([]string, 0, len(items))
	for _, it := range items {
		key := changeGroupKey(it)
		group, ok := groups[key]
		if !ok {
			groups[key] = &changeGroup{
				newest:      it,
				occurrences: 1,
				firstSeen:   it.Time,
				lastSeen:    it.Time,
			}
			keys = append(keys, key)
			continue
		}
		group.occurrences++
		if it.Time > group.newest.Time {
			group.newest = it
		}
		if it.Time < group.firstSeen {
			group.firstSeen = it.Time
		}
		if it.Time > group.lastSeen {
			group.lastSeen = it.Time
		}
	}

	out := make([]changeItemDTO, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		merged := group.newest
		merged.Occurrences = group.occurrences
		merged.FirstSeen = group.firstSeen
		merged.LastSeen = group.lastSeen
		out = append(out, merged)
	}
	return out
}

// changeStats aggregates window-wide statistics over the FULL source result
// (before any filter, grouping or page), so the console cards and type tabs
// stay honest and stable while the user filters.
func changeStats(items []changeItemDTO) map[string]interface{} {
	byKind := map[string]int{}
	services := map[string]int{}
	systems := map[string]int{}
	namespaces := map[string]int{}
	levelCounts := map[string]int{}
	for _, it := range items {
		kind := it.Kind
		if kind == "" {
			kind = "unknown"
		}
		byKind[kind]++
		if it.Service != "" {
			services[it.Service]++
		}
		if it.System != "" {
			systems[it.System]++
		}
		if it.Namespace != "" {
			namespaces[it.Namespace]++
		}
		level := it.Level
		if level == "" {
			level = "unknown"
		}
		levelCounts[level]++
	}

	return map[string]interface{}{
		"total":        len(items),
		"by_kind":      byKind,
		"services":     len(services),
		"top_services": topNames(services, 3),
		"systems":      sortedNames(systems),
		"namespaces":   sortedNames(namespaces),
		"level_counts": levelCounts,
	}
}

// topNames returns the most frequent names, ties broken alphabetically, capped
// at max entries.
func topNames(counts map[string]int, max int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > max {
		names = names[:max]
	}
	return names
}

// sortedNames returns the map keys in stable alphabetical order.
func sortedNames(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

const changeCacheTTL = 60 * time.Second

func (s *APIServer) changeCacheGet(key string) ([]changeItemDTO, map[string]interface{}, bool) {
	s.changeCacheMu.Lock()
	defer s.changeCacheMu.Unlock()
	if s.changeCacheSrc != s.changeSource || s.changeCacheKey != key ||
		time.Since(s.changeCacheAt) > changeCacheTTL {
		return nil, nil, false
	}
	return s.changeCacheItems, s.changeCacheStats, true
}

func (s *APIServer) changeCacheSet(key string, items []changeItemDTO, stats map[string]interface{}) {
	s.changeCacheMu.Lock()
	defer s.changeCacheMu.Unlock()
	s.changeCacheSrc = s.changeSource
	s.changeCacheKey = key
	s.changeCacheAt = time.Now()
	s.changeCacheItems = items
	s.changeCacheStats = stats
}
