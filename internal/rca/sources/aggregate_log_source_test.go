package sources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// fakeLogSource is a controllable LogSource for aggregate tests.
type fakeLogSource struct {
	entries []models.LogEntry
	count   int64
	err     error
}

func (f *fakeLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func (f *fakeLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func (f *fakeLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}

func ts(sec int) time.Time { return time.Unix(int64(sec), 0) }

func TestAggregateMergesAndSortsDescending(t *testing.T) {
	a := NewAggregateLogSource(nil,
		NamedLogSource{Name: "sls", Source: &fakeLogSource{entries: []models.LogEntry{
			{Timestamp: ts(100), Message: "a"},
			{Timestamp: ts(300), Message: "c"},
		}}},
		NamedLogSource{Name: "tls", Source: &fakeLogSource{entries: []models.LogEntry{
			{Timestamp: ts(200), Message: "b"},
		}}},
	)

	got, err := a.QueryErrors(context.Background(), ts(0), ts(1000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 merged entries, got %d", len(got))
	}
	// newest first
	if got[0].Message != "c" || got[1].Message != "b" || got[2].Message != "a" {
		t.Errorf("wrong order: %q, %q, %q", got[0].Message, got[1].Message, got[2].Message)
	}
}

func TestAggregateTagsSourceLabel(t *testing.T) {
	a := NewAggregateLogSource(nil,
		NamedLogSource{Name: "aliyun-sls", Source: &fakeLogSource{entries: []models.LogEntry{
			{Timestamp: ts(100), Message: "x"},
		}}},
		NamedLogSource{Name: "volc-tls", Source: &fakeLogSource{entries: []models.LogEntry{
			{Timestamp: ts(200), Message: "y"},
		}}},
	)

	got, err := a.QueryErrors(context.Background(), ts(0), ts(1000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// got[0] is newest (y from volc-tls), got[1] is x from aliyun-sls
	if got[0].Labels[SourceLabelKey] != "volc-tls" {
		t.Errorf("expected volc-tls label, got %q", got[0].Labels[SourceLabelKey])
	}
	if got[1].Labels[SourceLabelKey] != "aliyun-sls" {
		t.Errorf("expected aliyun-sls label, got %q", got[1].Labels[SourceLabelKey])
	}
}

func TestAggregateToleratesPartialFailure(t *testing.T) {
	a := NewAggregateLogSource(nil,
		NamedLogSource{Name: "healthy", Source: &fakeLogSource{entries: []models.LogEntry{
			{Timestamp: ts(100), Message: "ok"},
		}}},
		NamedLogSource{Name: "broken", Source: &fakeLogSource{err: errors.New("boom")}},
	)

	got, err := a.QueryErrors(context.Background(), ts(0), ts(1000))
	if err != nil {
		t.Fatalf("partial failure should not error: %v", err)
	}
	if len(got) != 1 || got[0].Message != "ok" {
		t.Fatalf("expected only the healthy source's entry, got %+v", got)
	}
}

func TestAggregateErrorsWhenAllFail(t *testing.T) {
	a := NewAggregateLogSource(nil,
		NamedLogSource{Name: "a", Source: &fakeLogSource{err: errors.New("boom1")}},
		NamedLogSource{Name: "b", Source: &fakeLogSource{err: errors.New("boom2")}},
	)

	_, err := a.QueryErrors(context.Background(), ts(0), ts(1000))
	if err == nil {
		t.Fatal("expected error when all sources fail")
	}
}

func TestAggregateGetLogCountSumsAndTolerates(t *testing.T) {
	a := NewAggregateLogSource(nil,
		NamedLogSource{Name: "a", Source: &fakeLogSource{count: 10}},
		NamedLogSource{Name: "b", Source: &fakeLogSource{count: 5}},
		NamedLogSource{Name: "broken", Source: &fakeLogSource{err: errors.New("boom")}},
	)

	n, err := a.GetLogCount(context.Background(), "svc", ts(0), ts(1000))
	if err != nil {
		t.Fatalf("partial failure should not error: %v", err)
	}
	if n != 15 {
		t.Errorf("expected summed count 15, got %d", n)
	}
}

func TestAggregateEmptyIsNotError(t *testing.T) {
	a := NewAggregateLogSource(nil)
	got, err := a.QueryErrors(context.Background(), ts(0), ts(1000))
	if err != nil {
		t.Fatalf("empty aggregate should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no entries, got %d", len(got))
	}
}
