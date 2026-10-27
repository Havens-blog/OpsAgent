package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

type fakeStreamClient struct {
	chunks []llm.ChatChunk
	err    error
}

func (f *fakeStreamClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeStreamClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeStreamClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) error {
	if f.err != nil {
		return f.err
	}
	for _, c := range f.chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		handler(c)
	}
	return nil
}

type blockingClient struct{}

func (b *blockingClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return nil, nil
}

func (b *blockingClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return nil, nil
}

func (b *blockingClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestServiceChatStreamDeltas(t *testing.T) {
	svc := NewServiceWithClient(&fakeStreamClient{
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "你"}}}},
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "好"}}}},
		},
	}, "test")

	var got string
	err := svc.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(d string) error {
		got += d
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if got != "你好" {
		t.Fatalf("got %q, want %q", got, "你好")
	}
}

func TestServiceChatStreamError(t *testing.T) {
	wantErr := errors.New("boom")
	svc := NewServiceWithClient(&fakeStreamClient{err: wantErr}, "test")

	err := svc.ChatStream(context.Background(), nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestServiceChatStreamCancel(t *testing.T) {
	svc := NewServiceWithClient(&blockingClient{}, "test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.ChatStream(ctx, nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
}
