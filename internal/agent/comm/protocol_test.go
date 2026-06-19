// Package comm provides tests for the inter-agent communication protocol.
package comm

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestBrokerRegisterUnregister(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	t.Run("Register and List", func(t *testing.T) {
		mailbox, err := broker.Register("agent-a", "LogAnalyst", "log_analyst", 10)
		if err != nil {
			t.Fatalf("Failed to register: %v", err)
		}

		if mailbox == nil {
			t.Fatal("Expected mailbox, got nil")
		}

		agents := broker.ListAgents()
		if len(agents) != 1 {
			t.Errorf("Expected 1 agent, got %d", len(agents))
		}
	})

	t.Run("Duplicate Registration Fails", func(t *testing.T) {
		_, err := broker.Register("agent-a", "LogAnalyst", "log_analyst", 10)
		if err == nil {
			t.Error("Expected error on duplicate registration")
		}
	})

	t.Run("Unregister", func(t *testing.T) {
		err := broker.Unregister("agent-a")
		if err != nil {
			t.Fatalf("Failed to unregister: %v", err)
		}

		agents := broker.ListAgents()
		if len(agents) != 0 {
			t.Errorf("Expected 0 agents after unregister, got %d", len(agents))
		}
	})

	t.Run("Unregister Unknown Fails", func(t *testing.T) {
		err := broker.Unregister("nonexistent")
		if err == nil {
			t.Error("Expected error unregistering unknown agent")
		}
	})

	_ = ctx
}

func TestBrokerSend(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	mailboxA, _ := broker.Register("agent-a", "A", "test", 10)
	broker.Register("agent-b", "B", "test", 10)

	t.Run("Direct Send", func(t *testing.T) {
		msg := NewQueryMessage("agent-a", "agent-b", "status check", "are you ok?")
		err := broker.Send(ctx, msg)
		if err != nil {
			t.Fatalf("Failed to send: %v", err)
		}

		// agent-b should receive
		bMailbox, _ := broker.Register("agent-c", "C", "test", 10)
		_ = bMailbox

		// Verify metrics
		metrics := broker.GetMetrics()
		if metrics.TotalSent == 0 {
			t.Error("Expected sent count > 0")
		}
		if metrics.TotalDelivered == 0 {
			t.Error("Expected delivered count > 0")
		}
	})

	t.Run("Send to Unknown Agent Fails", func(t *testing.T) {
		msg := NewQueryMessage("agent-a", "nonexistent", "test", nil)
		err := broker.Send(ctx, msg)
		if err == nil {
			t.Error("Expected error sending to unknown agent")
		}
	})

	t.Run("Send Nil Message Fails", func(t *testing.T) {
		err := broker.Send(ctx, nil)
		if err == nil {
			t.Error("Expected error sending nil message")
		}
	})

	_ = mailboxA
}

func TestBrokerBroadcast(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	broker.Register("agent-a", "A", "test", 10)
	mailboxB, _ := broker.Register("agent-b", "B", "test", 10)
	mailboxC, _ := broker.Register("agent-c", "C", "test", 10)

	t.Run("Broadcast Reaches All Except Sender", func(t *testing.T) {
		msg := NewBroadcastMessage("agent-a", MessageTypeEvidenceShare, "evidence", "data")
		err := broker.Send(ctx, msg)
		if err != nil {
			t.Fatalf("Failed to broadcast: %v", err)
		}

		// Give a moment for delivery
		time.Sleep(10 * time.Millisecond)

		// agent-b and agent-c should have one message each
		if mailboxB.Len() != 1 {
			t.Errorf("Expected agent-b to have 1 message, got %d", mailboxB.Len())
		}
		if mailboxC.Len() != 1 {
			t.Errorf("Expected agent-c to have 1 message, got %d", mailboxC.Len())
		}
	})
}

func TestDeliveryExactlyOnce(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	broker.Register("agent-a", "A", "test", 10)
	broker.Register("agent-b", "B", "test", 10)

	t.Run("Duplicate Message is Deduped", func(t *testing.T) {
		msg := &Message{
			From:              "agent-a",
			To:                "agent-b",
			Type:              MessageTypeQuery,
			Subject:           "test",
			Payload:           "data",
			DeliveryGuarantee: DeliveryExactlyOnce,
			IdempotencyKey:    "op-123",
		}

		// First send succeeds
		err := broker.Send(ctx, msg)
		if err != nil {
			t.Fatalf("First send failed: %v", err)
		}

		// Second send with same idempotency key should be deduped (no error, no delivery)
		msg2 := &Message{
			From:              "agent-a",
			To:                "agent-b",
			Type:              MessageTypeQuery,
			Subject:           "test",
			Payload:           "data",
			DeliveryGuarantee: DeliveryExactlyOnce,
			IdempotencyKey:    "op-123",
		}
		err = broker.Send(ctx, msg2)
		if err != nil {
			t.Fatalf("Deduped send should not error: %v", err)
		}

		metrics := broker.GetMetrics()
		if metrics.TotalDeduped == 0 {
			t.Error("Expected dedup count > 0")
		}
	})
}

func TestTTLExpiry(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	broker.Register("agent-a", "A", "test", 10)
	broker.Register("agent-b", "B", "test", 10)

	t.Run("Expired Message Fails", func(t *testing.T) {
		msg := &Message{
			From:       "agent-a",
			To:         "agent-b",
			Type:       MessageTypeQuery,
			Subject:    "expired",
			Payload:    "data",
			ExpiresAt:  time.Now().Add(-1 * time.Minute), // Already expired
		}

		err := broker.Send(ctx, msg)
		if err == nil {
			t.Error("Expected error for expired message")
		}
	})
}

func TestMailbox(t *testing.T) {
	t.Run("Receive Returns Message", func(t *testing.T) {
		mailbox := NewMailbox("agent-x", 10)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		msg := &Message{ID: "test-1", To: "agent-x", From: "agent-y"}
		_ = mailbox.deliver(msg)

		received, err := mailbox.Receive(ctx)
		if err != nil {
			t.Fatalf("Failed to receive: %v", err)
		}

		if received.ID != "test-1" {
			t.Errorf("Expected message ID test-1, got %s", received.ID)
		}
	})

	t.Run("TryReceive Returns Nil When Empty", func(t *testing.T) {
		mailbox := NewMailbox("agent-x", 10)
		msg := mailbox.TryReceive()
		if msg != nil {
			t.Error("Expected nil from empty mailbox")
		}
	})

	t.Run("Full Mailbox Rejects", func(t *testing.T) {
		mailbox := NewMailbox("agent-x", 1)
		_ = mailbox.deliver(&Message{ID: "1"})
		err := mailbox.deliver(&Message{ID: "2"})
		if err == nil {
			t.Error("Expected error when mailbox is full")
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		mailbox := NewMailbox("agent-x", 10)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := mailbox.Receive(ctx)
		if err == nil {
			t.Error("Expected error on context cancellation")
		}
	})
}

func TestSendSync(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	// Register requester and responder
	_, _ = broker.Register("agent-a", "A", "test", 10)
	responderMailbox, _ := broker.Register("agent-b", "B", "test", 10)

	// Responder goroutine: receive query, send response
	go func() {
		req, err := responderMailbox.Receive(context.Background())
		if err != nil {
			return
		}

		resp := NewResponseMessage("agent-b", "agent-a", req.ID, req.ID, "response-data")
		_ = broker.Send(context.Background(), resp)
	}()

	t.Run("SendSync Receives Response", func(t *testing.T) {
		req := NewQueryMessage("agent-a", "agent-b", "query", "hello")
		resp, err := broker.SendSync(ctx, req, 2*time.Second)
		if err != nil {
			t.Fatalf("SendSync failed: %v", err)
		}

		if resp == nil {
			t.Fatal("Expected response, got nil")
		}

		if resp.Payload != "response-data" {
			t.Errorf("Expected payload 'response-data', got %v", resp.Payload)
		}
	})
}

func TestSubscribe(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	broker.Register("agent-a", "A", "test", 10)
	broker.Register("agent-b", "B", "test", 10)

	t.Run("Subscribe and Notify", func(t *testing.T) {
		var received []*Message
		var mu sync.Mutex

		cleanup := broker.Subscribe("req-123", func(msg *Message) {
			mu.Lock()
			received = append(received, msg)
			mu.Unlock()
		})
		defer cleanup()

		// Send a response with matching correlation ID
		resp := &Message{
			From:          "agent-b",
			To:            "agent-a",
			Type:          MessageTypeResponse,
			CorrelationID: "req-123",
			Payload:       "reply",
		}

		err := broker.Send(ctx, resp)
		if err != nil {
			t.Fatalf("Failed to send: %v", err)
		}

		// Give handler time to run
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		if len(received) != 1 {
			t.Errorf("Expected 1 received message, got %d", len(received))
		}
		mu.Unlock()
	})

	t.Run("Unsubscribe Stops Notifications", func(t *testing.T) {
		var count int
		var mu sync.Mutex

		cleanup := broker.Subscribe("req-456", func(msg *Message) {
			mu.Lock()
			count++
			mu.Unlock()
		})

		// Send first message
		resp1 := &Message{
			From:          "agent-b",
			To:            "agent-a",
			CorrelationID: "req-456",
		}
		_ = broker.Send(ctx, resp1)
		time.Sleep(50 * time.Millisecond)

		// Unsubscribe
		cleanup()

		// Send second message
		resp2 := &Message{
			From:          "agent-b",
			To:            "agent-a",
			CorrelationID: "req-456",
		}
		_ = broker.Send(ctx, resp2)
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		if count != 1 {
			t.Errorf("Expected count 1 after unsubscribe, got %d", count)
		}
		mu.Unlock()
	})
}

func TestMessageConstructors(t *testing.T) {
	t.Run("NewQueryMessage", func(t *testing.T) {
		msg := NewQueryMessage("a", "b", "test", "payload")
		if msg.Type != MessageTypeQuery {
			t.Errorf("Expected type %s, got %s", MessageTypeQuery, msg.Type)
		}
		if msg.From != "a" || msg.To != "b" {
			t.Error("From/To mismatch")
		}
		if msg.TTL == 0 {
			t.Error("Expected non-zero TTL")
		}
	})

	t.Run("NewResponseMessage", func(t *testing.T) {
		msg := NewResponseMessage("b", "a", "req-1", "corr-1", "data")
		if msg.Type != MessageTypeResponse {
			t.Errorf("Expected type %s, got %s", MessageTypeResponse, msg.Type)
		}
		if msg.ReplyToID != "req-1" {
			t.Errorf("Expected ReplyToID req-1, got %s", msg.ReplyToID)
		}
	})

	t.Run("NewBroadcastMessage", func(t *testing.T) {
		msg := NewBroadcastMessage("a", MessageTypeEvidenceShare, "evidence", "data")
		if msg.To != "*" {
			t.Errorf("Expected To *, got %s", msg.To)
		}
		if msg.Type != MessageTypeEvidenceShare {
			t.Errorf("Expected type %s, got %s", MessageTypeEvidenceShare, msg.Type)
		}
	})

	t.Run("NewEvidenceShareMessage", func(t *testing.T) {
		msg := NewEvidenceShareMessage("a", "evidence")
		if msg.Type != MessageTypeEvidenceShare {
			t.Errorf("Expected type %s, got %s", MessageTypeEvidenceShare, msg.Type)
		}
		if msg.Priority != PriorityHigh {
			t.Errorf("Expected priority %d, got %d", PriorityHigh, msg.Priority)
		}
		if msg.DeliveryGuarantee != DeliveryAtLeastOnce {
			t.Errorf("Expected delivery %s, got %s", DeliveryAtLeastOnce, msg.DeliveryGuarantee)
		}
	})
}

func TestBrokerMetrics(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	broker.Register("agent-a", "A", "test", 10)
	broker.Register("agent-b", "B", "test", 10)

	// Send some messages
	for i := 0; i < 5; i++ {
		_ = broker.Send(ctx, NewQueryMessage("agent-a", "agent-b", "test", i))
	}

	metrics := broker.GetMetrics()

	if metrics.TotalSent != 5 {
		t.Errorf("Expected 5 sent, got %d", metrics.TotalSent)
	}
	if metrics.TotalDelivered != 5 {
		t.Errorf("Expected 5 delivered, got %d", metrics.TotalDelivered)
	}
}

func TestGetAgent(t *testing.T) {
	broker := NewBroker()

	broker.Register("agent-a", "LogAnalyst", "log_analyst", 10)

	t.Run("Get Existing Agent", func(t *testing.T) {
		endpoint, err := broker.GetAgent("agent-a")
		if err != nil {
			t.Fatalf("Failed to get agent: %v", err)
		}

		if endpoint.ID != "agent-a" {
			t.Errorf("Expected ID agent-a, got %s", endpoint.ID)
		}
		if endpoint.Name != "LogAnalyst" {
			t.Errorf("Expected name LogAnalyst, got %s", endpoint.Name)
		}
	})

	t.Run("Get Unknown Agent Fails", func(t *testing.T) {
		_, err := broker.GetAgent("nonexistent")
		if err == nil {
			t.Error("Expected error for unknown agent")
		}
	})
}

func TestCarbonCopy(t *testing.T) {
	broker := NewBroker()
	ctx := context.Background()

	broker.Register("agent-a", "A", "test", 10)
	mailboxB, _ := broker.Register("agent-b", "B", "test", 10)
	mailboxC, _ := broker.Register("agent-c", "C", "test", 10)

	t.Run("Carbon Copy Delivers to CC", func(t *testing.T) {
		msg := NewQueryMessage("agent-a", "agent-b", "test", "data")
		msg.Cc = []string{"agent-c"}

		err := broker.Send(ctx, msg)
		if err != nil {
			t.Fatalf("Failed to send: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		// Both primary and CC should receive
		if mailboxB.Len() != 1 {
			t.Errorf("Expected agent-b to have 1 message, got %d", mailboxB.Len())
		}
		if mailboxC.Len() != 1 {
			t.Errorf("Expected agent-c (CC) to have 1 message, got %d", mailboxC.Len())
		}
	})
}

func TestMessagePriority(t *testing.T) {
	t.Run("Priority Values", func(t *testing.T) {
		if PriorityLow >= PriorityNormal {
			t.Error("PriorityLow should be < PriorityNormal")
		}
		if PriorityNormal >= PriorityHigh {
			t.Error("PriorityNormal should be < PriorityHigh")
		}
		if PriorityHigh >= PriorityUrgent {
			t.Error("PriorityHigh should be < PriorityUrgent")
		}
	})
}

func TestMessageType(t *testing.T) {
	t.Run("Message Type Values", func(t *testing.T) {
		types := []MessageType{
			MessageTypeQuery,
			MessageTypeResponse,
			MessageTypeBroadcast,
			MessageTypeEvidenceShare,
			MessageTypeConflictReport,
			MessageTypeTaskRequest,
			MessageTypeTaskResult,
			MessageTypeHypothesis,
			MessageTypeVote,
			MessageTypeHeartbeat,
		}

		seen := make(map[MessageType]bool)
		for _, mt := range types {
			if mt == "" {
				t.Error("Message type should not be empty")
			}
			if seen[mt] {
				t.Errorf("Duplicate message type: %s", mt)
			}
			seen[mt] = true
		}
	})
}
