// Package comm provides inter-agent communication protocol for multi-agent collaboration.
// This module defines message format, routing, delivery guarantees, and the message broker.
package comm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MessageType represents the type of inter-agent message.
type MessageType string

const (
	// MessageTypeQuery is a request for information from another agent
	MessageTypeQuery MessageType = "query"
	// MessageTypeResponse is a reply to a query
	MessageTypeResponse MessageType = "response"
	// MessageTypeBroadcast is a message sent to all agents
	MessageTypeBroadcast MessageType = "broadcast"
	// MessageTypeEvidenceShare is an agent sharing collected evidence
	MessageTypeEvidenceShare MessageType = "evidence_share"
	// MessageTypeConflictReport is a report of conflicting conclusions
	MessageTypeConflictReport MessageType = "conflict_report"
	// MessageTypeTaskRequest is a request to delegate a task
	MessageTypeTaskRequest MessageType = "task_request"
	// MessageTypeTaskResult is the result of a delegated task
	MessageTypeTaskResult MessageType = "task_result"
	// MessageTypeHypothesis is a root cause hypothesis
	MessageTypeHypothesis MessageType = "hypothesis"
	// MessageTypeVote is a vote in a conflict resolution
	MessageTypeVote MessageType = "vote"
	// MessageTypeHeartbeat is a liveness signal
	MessageTypeHeartbeat MessageType = "heartbeat"
)

// DeliveryGuarantee represents the delivery semantics for a message.
type DeliveryGuarantee string

const (
	// DeliveryAtMostOnce sends at most once (fire-and-forget)
	DeliveryAtMostOnce DeliveryGuarantee = "at_most_once"
	// DeliveryAtLeastOnce retries until acknowledged
	DeliveryAtLeastOnce DeliveryGuarantee = "at_least_once"
	// DeliveryExactlyOnce ensures no duplicates via idempotency keys
	DeliveryExactlyOnce DeliveryGuarantee = "exactly_once"
)

// MessagePriority represents the priority of a message.
type MessagePriority int

const (
	PriorityLow    MessagePriority = 0
	PriorityNormal MessagePriority = 1
	PriorityHigh   MessagePriority = 2
	PriorityUrgent MessagePriority = 3
)

// Message is the core inter-agent communication unit.
type Message struct {
	// Identification
	ID            string      `json:"id"`
	CorrelationID string      `json:"correlation_id,omitempty"` // Ties responses to requests
	ReplyToID     string      `json:"reply_to_id,omitempty"`     // ID of message this replies to

	// Routing
	From    string   `json:"from"`           // Sending agent ID
	To      string   `json:"to"`             // Recipient agent ID (or "*" for broadcast)
	Cc      []string `json:"cc,omitempty"`   // Carbon-copy recipients

	// Content
	Type    MessageType `json:"type"`
	Subject string      `json:"subject"`
	Payload interface{} `json:"payload"`

	// Metadata
	Priority        MessagePriority  `json:"priority"`
	DeliveryGuarantee DeliveryGuarantee `json:"delivery_guarantee"`
	IdempotencyKey  string           `json:"idempotency_key,omitempty"` // For exactly-once dedup
	TTL             time.Duration    `json:"ttl"`                       // Time-to-live

	// Timing
	Timestamp time.Time `json:"timestamp"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// Tracing
	TraceID    string   `json:"trace_id,omitempty"`
	Hops       []string `json:"hops,omitempty"` // Agents that handled this message
}

// Envelope wraps a Message with delivery metadata.
type Envelope struct {
	Message       *Message
	DeliveredAt   time.Time
	AcknowledgedAt time.Time
	RetryCount    int
	MaxRetries    int
	LastError     error
}

// Mailbox is a per-agent message inbox.
type Mailbox struct {
	agentID  string
	queue    chan *Message
	capacity int
	mu       sync.Mutex
	closed   bool
}

// NewMailbox creates a new mailbox for an agent.
func NewMailbox(agentID string, capacity int) *Mailbox {
	if capacity <= 0 {
		capacity = 100
	}
	return &Mailbox{
		agentID:  agentID,
		queue:    make(chan *Message, capacity),
		capacity: capacity,
	}
}

// Receive blocks until a message is available or context is cancelled.
func (m *Mailbox) Receive(ctx context.Context) (*Message, error) {
	select {
	case msg, ok := <-m.queue:
		if !ok {
			return nil, fmt.Errorf("mailbox closed")
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TryReceive returns a message without blocking, or nil if none available.
func (m *Mailbox) TryReceive() *Message {
	select {
	case msg := <-m.queue:
		return msg
	default:
		return nil
	}
}

// deliver puts a message into the mailbox.
func (m *Mailbox) deliver(msg *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("mailbox for %s is closed", m.agentID)
	}

	select {
	case m.queue <- msg:
		return nil
	default:
		return fmt.Errorf("mailbox for %s is full", m.agentID)
	}
}

// Close closes the mailbox.
func (m *Mailbox) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.closed = true
		close(m.queue)
	}
}

// Len returns the number of messages in the mailbox.
func (m *Mailbox) Len() int {
	return len(m.queue)
}

// AgentEndpoint represents a registered agent in the broker.
type AgentEndpoint struct {
	ID          string
	Name        string
	Type        string
	Mailbox     *Mailbox
	RegisteredAt time.Time
	LastSeen    time.Time
}

// Broker is the message broker that routes messages between agents.
type Broker struct {
	mu            sync.RWMutex
	endpoints     map[string]*AgentEndpoint
	delivered     sync.Map // idempotency tracking: key -> delivery time
	metrics       *BrokerMetrics
	metricsMu     sync.Mutex
	subscriptions *sync.Map // correlationID -> *[]*subscriptionEntry
}

// BrokerMetrics tracks broker delivery statistics.
type BrokerMetrics struct {
	TotalSent      int64
	TotalDelivered int64
	TotalFailed    int64
	TotalRetried   int64
	TotalDeduped   int64
}

// NewBroker creates a new message broker.
func NewBroker() *Broker {
	return &Broker{
		endpoints:     make(map[string]*AgentEndpoint),
		metrics:       &BrokerMetrics{},
		subscriptions: &sync.Map{},
	}
}

// Register registers an agent with the broker.
func (b *Broker) Register(id, name, agentType string, mailboxCapacity int) (*Mailbox, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.endpoints[id]; exists {
		return nil, fmt.Errorf("agent %s already registered", id)
	}

	mailbox := NewMailbox(id, mailboxCapacity)
	b.endpoints[id] = &AgentEndpoint{
		ID:           id,
		Name:         name,
		Type:         agentType,
		Mailbox:      mailbox,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	return mailbox, nil
}

// Unregister removes an agent from the broker.
func (b *Broker) Unregister(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	endpoint, exists := b.endpoints[id]
	if !exists {
		return fmt.Errorf("agent %s not registered", id)
	}

	endpoint.Mailbox.Close()
	delete(b.endpoints, id)

	return nil
}

// Send delivers a message to its recipient(s).
func (b *Broker) Send(ctx context.Context, msg *Message) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}

	// Stamp message if fields missing
	b.stampMessage(msg)

	atomic.AddInt64(&b.metrics.TotalSent, 1)

	// Check exactly-once deduplication
	if msg.DeliveryGuarantee == DeliveryExactlyOnce {
		if b.isDuplicate(msg) {
			atomic.AddInt64(&b.metrics.TotalDeduped, 1)
			return nil // Already delivered
		}
	}

	// Check TTL expiry
	if !msg.ExpiresAt.IsZero() && time.Now().After(msg.ExpiresAt) {
		atomic.AddInt64(&b.metrics.TotalFailed, 1)
		return fmt.Errorf("message expired")
	}

	// Record hop
	msg.Hops = append(msg.Hops, msg.From)

	// Route message
	var err error
	if msg.To == "*" {
		err = b.broadcast(ctx, msg)
	} else {
		err = b.deliverTo(ctx, msg, msg.To)
	}

	// Notify any subscribers waiting on this message's correlation/reply ID.
	// This fires for responses (which carry ReplyToID/CorrelationID) so that
	// SendSync and other request/response flows can resolve.
	if err == nil {
		b.NotifySubscribers(msg)
	}

	// Forward carbon copies after primary delivery
	if err == nil && len(msg.Cc) > 0 {
		b.ForwardCarbonCopies(ctx, msg)
	}

	return err
}

// SendSync sends a message and waits for the response.
func (b *Broker) SendSync(ctx context.Context, msg *Message, timeout time.Duration) (*Message, error) {
	// Pre-stamp so we know the request ID before subscribing.
	// stampMessage is idempotent — the later Send() won't change a set ID.
	b.stampMessage(msg)

	// Subscribe to replies keyed by the request ID.
	replyCh := make(chan *Message, 1)
	cleanup := b.Subscribe(msg.ID, func(reply *Message) {
		select {
		case replyCh <- reply:
		default:
		}
	})
	defer cleanup()

	// Send the request
	if err := b.Send(ctx, msg); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case reply := <-replyCh:
		return reply, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for response")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// broadcast delivers a message to all registered agents except the sender.
func (b *Broker) broadcast(ctx context.Context, msg *Message) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var firstErr error
	delivered := 0

	for id, endpoint := range b.endpoints {
		if id == msg.From {
			continue // Don't send to self
		}

		if err := endpoint.Mailbox.deliver(msg); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			delivered++
			endpoint.LastSeen = time.Now()
		}
	}

	atomic.AddInt64(&b.metrics.TotalDelivered, int64(delivered))

	if delivered == 0 && firstErr != nil {
		atomic.AddInt64(&b.metrics.TotalFailed, 1)
		return firstErr
	}

	return nil
}

// deliverTo delivers a message to a specific agent.
func (b *Broker) deliverTo(ctx context.Context, msg *Message, agentID string) error {
	b.mu.RLock()
	endpoint, exists := b.endpoints[agentID]
	b.mu.RUnlock()

	if !exists {
		atomic.AddInt64(&b.metrics.TotalFailed, 1)
		return fmt.Errorf("agent %s not registered", agentID)
	}

	// Deliver with retry for at-least-once / exactly-once
	maxRetries := 0
	switch msg.DeliveryGuarantee {
	case DeliveryAtLeastOnce:
		maxRetries = 3
	case DeliveryExactlyOnce:
		maxRetries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			atomic.AddInt64(&b.metrics.TotalRetried, 1)
			select {
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if err := endpoint.Mailbox.deliver(msg); err != nil {
			lastErr = err
			continue
		}

		endpoint.LastSeen = time.Now()
		atomic.AddInt64(&b.metrics.TotalDelivered, 1)
		return nil
	}

	atomic.AddInt64(&b.metrics.TotalFailed, 1)
	return fmt.Errorf("failed to deliver after %d retries: %w", maxRetries, lastErr)
}

// ForwardCarbonCopies delivers the message to all Cc recipients.
func (b *Broker) ForwardCarbonCopies(ctx context.Context, msg *Message) {
	for _, cc := range msg.Cc {
		// Deliver to cc; ignore errors for carbon copies
		_ = b.deliverTo(ctx, msg, cc)
	}
}

// Subscription handler function type.
type MessageHandler func(*Message)

// subscriptionEntry tracks active subscriptions.
type subscriptionEntry struct {
	id      string
	handler MessageHandler
}

// Subscribe registers a handler for messages with a specific correlation ID.
// Returns a cleanup function to unsubscribe.
func (b *Broker) Subscribe(correlationID string, handler MessageHandler) func() {
	// Subscriptions are tracked via a dedicated internal agent
	// For simplicity, we use a sync.Map of correlationID -> handlers
	b.initSubscriptions()

	handlers := b.getOrCreateSubscriptionList(correlationID)
	entry := &subscriptionEntry{
		id:      generateSubscriptionID(),
		handler: handler,
	}

	b.mu.Lock()
	*handlers = append(*handlers, entry)
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		filtered := (*handlers)[:0]
		for _, e := range *handlers {
			if e.id != entry.id {
				filtered = append(filtered, e)
			}
		}
		*handlers = filtered
		b.mu.Unlock()
	}
}

// NotifySubscribers invokes handlers matching a message's correlation/reply ID.
func (b *Broker) NotifySubscribers(msg *Message) {
	if b.subscriptions == nil {
		return
	}

	key := msg.CorrelationID
	if key == "" {
		key = msg.ReplyToID
	}
	if key == "" {
		return
	}

	raw, ok := b.subscriptions.Load(key)
	if !ok {
		return
	}

	handlers := raw.(*[]*subscriptionEntry)
	// Snapshot under lock to avoid mutation during iteration
	b.mu.Lock()
	snapshot := make([]*subscriptionEntry, len(*handlers))
	copy(snapshot, *handlers)
	b.mu.Unlock()

	for _, entry := range snapshot {
		entry.handler(msg)
	}
}

// subscriptions state (lazily initialized)
var (
	subscriptionCounter int64
)

func (b *Broker) initSubscriptions() {
	if b.subscriptions == nil {
		b.subscriptions = &sync.Map{}
	}
}

func (b *Broker) getOrCreateSubscriptionList(correlationID string) *[]*subscriptionEntry {
	raw, _ := b.subscriptions.LoadOrStore(correlationID, &[]*subscriptionEntry{})
	return raw.(*[]*subscriptionEntry)
}

func generateSubscriptionID() string {
	n := atomic.AddInt64(&subscriptionCounter, 1)
	return fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), n)
}

// stampMessage fills in default fields.
func (b *Broker) stampMessage(msg *Message) {
	if msg.ID == "" {
		msg.ID = generateMessageID()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.ExpiresAt.IsZero() && msg.TTL > 0 {
		msg.ExpiresAt = msg.Timestamp.Add(msg.TTL)
	}
	if msg.DeliveryGuarantee == "" {
		msg.DeliveryGuarantee = DeliveryAtMostOnce
	}
	if msg.IdempotencyKey == "" && msg.DeliveryGuarantee == DeliveryExactlyOnce {
		msg.IdempotencyKey = msg.ID
	}
}

// isDuplicate checks if an exactly-once message was already delivered.
func (b *Broker) isDuplicate(msg *Message) bool {
	key := msg.IdempotencyKey
	if key == "" {
		key = msg.ID
	}

	if _, loaded := b.delivered.LoadOrStore(key, time.Now()); loaded {
		return true
	}
	return false
}

// generateMessageID generates a unique message ID.
func generateMessageID() string {
	n := atomic.AddInt64(&messageCounter, 1)
	return fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), n)
}

var messageCounter int64

// GetMetrics returns a snapshot of broker metrics.
func (b *Broker) GetMetrics() BrokerMetrics {
	b.metricsMu.Lock()
	defer b.metricsMu.Unlock()

	return BrokerMetrics{
		TotalSent:      atomic.LoadInt64(&b.metrics.TotalSent),
		TotalDelivered: atomic.LoadInt64(&b.metrics.TotalDelivered),
		TotalFailed:    atomic.LoadInt64(&b.metrics.TotalFailed),
		TotalRetried:   atomic.LoadInt64(&b.metrics.TotalRetried),
		TotalDeduped:   atomic.LoadInt64(&b.metrics.TotalDeduped),
	}
}

// ListAgents returns all registered agent IDs.
func (b *Broker) ListAgents() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ids := make([]string, 0, len(b.endpoints))
	for id := range b.endpoints {
		ids = append(ids, id)
	}
	return ids
}

// GetAgent returns info about a registered agent.
func (b *Broker) GetAgent(id string) (*AgentEndpoint, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	endpoint, exists := b.endpoints[id]
	if !exists {
		return nil, fmt.Errorf("agent %s not registered", id)
	}

	// Return a copy to avoid external mutation
	copy := *endpoint
	return &copy, nil
}

// NewQueryMessage creates a query message.
func NewQueryMessage(from, to, subject string, payload interface{}) *Message {
	return &Message{
		From:              from,
		To:                to,
		Type:              MessageTypeQuery,
		Subject:           subject,
		Payload:           payload,
		Priority:          PriorityNormal,
		DeliveryGuarantee: DeliveryAtMostOnce,
		TTL:               5 * time.Minute,
	}
}

// NewResponseMessage creates a response message.
func NewResponseMessage(from, to, replyToID, correlationID string, payload interface{}) *Message {
	return &Message{
		From:              from,
		To:                to,
		ReplyToID:         replyToID,
		CorrelationID:     correlationID,
		Type:              MessageTypeResponse,
		Payload:           payload,
		Priority:          PriorityNormal,
		DeliveryGuarantee: DeliveryAtMostOnce,
	}
}

// NewBroadcastMessage creates a broadcast message.
func NewBroadcastMessage(from string, msgType MessageType, subject string, payload interface{}) *Message {
	return &Message{
		From:              from,
		To:                "*",
		Type:              msgType,
		Subject:           subject,
		Payload:           payload,
		Priority:          PriorityNormal,
		DeliveryGuarantee: DeliveryAtMostOnce,
	}
}

// NewEvidenceShareMessage creates an evidence-sharing message.
func NewEvidenceShareMessage(from string, evidence interface{}) *Message {
	return &Message{
		From:              from,
		To:                "*",
		Type:              MessageTypeEvidenceShare,
		Subject:           "Evidence sharing",
		Payload:           evidence,
		Priority:          PriorityHigh,
		DeliveryGuarantee: DeliveryAtLeastOnce,
	}
}
