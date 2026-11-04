package rca

import (
	"context"
	"sync"
	"testing"
)

// TestUpdateStatusDoesNotMutateReturnedReport Get 已返回的报告不应被后续 UpdateStatus 原地修改
func TestUpdateStatusDoesNotMutateReturnedReport(t *testing.T) {
	st := NewMemoryRCAReportStore()
	ctx := context.Background()
	if err := st.Store(ctx, report("inc")); err != nil {
		t.Fatalf("store: %v", err)
	}

	before, _ := st.Get(ctx, "inc")
	if err := st.UpdateStatus(ctx, "inc", "completed"); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := st.Get(ctx, "inc")

	if before.Incident.Labels["status"] != "running" {
		t.Errorf("previously returned report mutated: %v", before.Incident.Labels)
	}
	if after.Incident.Labels["status"] != "completed" {
		t.Errorf("status = %v, want completed", after.Incident.Labels["status"])
	}
}

// TestUpdateStatusConcurrentWithGet 并发更新状态与读取标签，配合 -race 检测数据竞争
func TestUpdateStatusConcurrentWithGet(t *testing.T) {
	st := NewMemoryRCAReportStore()
	ctx := context.Background()
	if err := st.Store(ctx, report("inc")); err != nil {
		t.Fatalf("store: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = st.UpdateStatus(ctx, "inc", "completed")
		}()
		go func() {
			defer wg.Done()
			if r, err := st.Get(ctx, "inc"); err == nil {
				_ = r.Incident.Labels["status"]
			}
		}()
	}
	wg.Wait()
}
