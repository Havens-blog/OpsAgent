package interfaces

import (
	"context"
	"sync"
	"testing"
)

// TestTaskStoreIsolatesCallerCopy 调用方在 Create/Update 之后继续修改自己的对象，不能影响存储内容
func TestTaskStoreIsolatesCallerCopy(t *testing.T) {
	st := NewMemoryAgentTaskStore()
	ctx := context.Background()

	task := &AgentTask{ID: "t1", Status: TaskStatusPending}
	if err := st.Create(ctx, task); err != nil {
		t.Fatalf("create: %v", err)
	}
	task.Status = TaskStatusRunning
	if got, _ := st.Get(ctx, "t1"); got.Status != TaskStatusPending {
		t.Errorf("Create kept caller pointer: status = %q", got.Status)
	}

	if err := st.Update(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}
	task.Status = TaskStatusFailed
	if got, _ := st.Get(ctx, "t1"); got.Status != TaskStatusRunning {
		t.Errorf("Update kept caller pointer: status = %q", got.Status)
	}
}

// TestTaskStoreConcurrentUpdateAndGet 模拟执行 goroutine 改状态、轮询方读取，配合 -race 检测
func TestTaskStoreConcurrentUpdateAndGet(t *testing.T) {
	st := NewMemoryAgentTaskStore()
	ctx := context.Background()
	task := &AgentTask{ID: "t1", Status: TaskStatusPending}
	if err := st.Create(ctx, task); err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			task.Status = TaskStatusRunning
			task.ApprovalID = "a"
			_ = st.Update(ctx, task)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if got, err := st.Get(ctx, "t1"); err == nil {
				_ = got.Status
				_ = got.ApprovalID
			}
		}
	}()
	wg.Wait()
}
