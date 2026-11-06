package interfaces

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postMemoryFact(t *testing.T, server *APIServer, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/memory", bytes.NewReader(b))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

func TestConfirmMemoryFactHandler(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	valid := map[string]interface{}{
		"kind":         "root_cause",
		"service":      "order-service",
		"content":      "连接池耗尽导致 5xx 飙升",
		"confidence":   0.8,
		"confirmed_by": "oncall-alice",
	}
	w := postMemoryFact(t, server, valid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", w.Code, w.Body.String())
	}
	var created struct {
		Stored    bool `json:"stored"`
		Duplicate bool `json:"duplicate"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !created.Stored || created.Duplicate {
		t.Fatalf("create response = %s", w.Body.String())
	}

	// 相同事实重复确认按幂等处理
	if again := postMemoryFact(t, server, valid); again.Code != http.StatusCreated {
		t.Errorf("duplicate status = %d, want 201", again.Code)
	} else {
		var res struct {
			Stored    bool `json:"stored"`
			Duplicate bool `json:"duplicate"`
		}
		_ = json.Unmarshal(again.Body.Bytes(), &res)
		if res.Stored || !res.Duplicate {
			t.Errorf("duplicate response = %s", again.Body.String())
		}
	}

	// 检索返回已确认事实，并支持版本过滤
	get := httptest.NewRequest(http.MethodGet, "/api/v1/memory?service=order-service", nil)
	w = httptest.NewRecorder()
	server.ServeHTTP(w, get)
	var listed struct {
		Count int `json:"count"`
		Facts []struct {
			Service     string `json:"service"`
			Content     string `json:"content"`
			ConfirmedBy string `json:"confirmed_by"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v (%s)", err, w.Body.String())
	}
	if w.Code != http.StatusOK || listed.Count != 1 || len(listed.Facts) != 1 ||
		listed.Facts[0].Content != "连接池耗尽导致 5xx 飙升" || listed.Facts[0].ConfirmedBy != "oncall-alice" {
		t.Errorf("list status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConfirmMemoryFactHandlerValidation(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	bad := []map[string]interface{}{
		{"kind": "unknown", "service": "s", "content": "c", "confidence": 0.8, "confirmed_by": "a"},
		{"kind": "root_cause", "service": "", "content": "c", "confidence": 0.8, "confirmed_by": "a"},
		{"kind": "root_cause", "service": "s", "content": "", "confidence": 0.8, "confirmed_by": "a"},
		{"kind": "root_cause", "service": "s", "content": "c", "confidence": 0.8},
		{"kind": "root_cause", "service": "s", "content": "c", "confidence": 1.5, "confirmed_by": "a"},
		{"kind": "root_cause", "service": "s", "content": "c", "confidence": 0.8, "confirmed_by": "a", "expires_at": "not-a-time"},
	}
	for i, body := range bad {
		if w := postMemoryFact(t, server, body); w.Code != http.StatusBadRequest {
			t.Errorf("case %d: status = %d, want 400 (body %s)", i, w.Code, w.Body.String())
		}
	}

	// 校验失败的事实绝不能入库
	get := httptest.NewRequest(http.MethodGet, "/api/v1/memory", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, get)
	var listed struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if listed.Count != 0 {
		t.Errorf("rejected facts leaked into memory: count = %d", listed.Count)
	}
}

func TestQueryMemoryVersionFilter(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	for _, v := range []string{"1.0", "2.0"} {
		if w := postMemoryFact(t, server, map[string]interface{}{
			"kind": "fragility", "service": "gateway", "content": "限流阈值偏低",
			"confidence": 0.6, "confirmed_by": "sre-bob", "version": v,
		}); w.Code != http.StatusCreated {
			t.Fatalf("create v%s: %d %s", v, w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/memory?service=gateway&version=1.0", nil))
	var listed struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if listed.Count != 1 {
		t.Errorf("version filter count = %d, want 1", listed.Count)
	}
}
