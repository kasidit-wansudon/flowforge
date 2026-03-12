// Package stream provides NATS JetStream-based event streaming with producer/consumer
// abstractions and an in-memory implementation for testing.
package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// StreamConfig holds NATS JetStream connection and stream parameters.
type StreamConfig struct {
	// URL is the NATS server connection URL (e.g. "nats://localhost:4222").
	URL string
	// StreamName is the JetStream stream name.
	StreamName string
	// Subjects is the list of subjects the stream captures.
	Subjects []string
	// MaxRetries is the maximum number of delivery attempts before a message
	// is considered failed and should be moved to a dead-letter queue.
	MaxRetries int
	// AckWait is the duration the server waits for an acknowledgement before
	// redelivering.
	AckWait time.Duration
}

// DefaultStreamConfig returns a StreamConfig with sensible defaults.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		URL:        nats.DefaultURL,
		StreamName: "FLOWFORGE",
		Subjects:   []string{"flowforge.>"},
		MaxRetries: 5,
		AckWait:    30 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// Message
// ---------------------------------------------------------------------------

// Message represents an event flowing through the stream.
type Message struct {
	// ID is a unique message identifier.
	ID string
	// Subject is the NATS subject this message was published on.
	Subject string
	// Data is the raw message payload.
	Data []byte
	// Timestamp records when the message was originally published.
	Timestamp time.Time
	// Headers holds optional key-value metadata.
	Headers map[string]string
	// RedeliveryCount tracks how many times the message has been delivered.
	RedeliveryCount int
}

// MessageHandler is the callback invoked for each message received by a consumer.
type MessageHandler func(msg *Message) error

// ---------------------------------------------------------------------------
// Producer interface
// ---------------------------------------------------------------------------

// Producer publishes messages to a stream.
type Producer interface {
	// Publish sends data to the given subject. It blocks until the server
	// acknowledges persistence (JetStream publish ack).
	Publish(ctx context.Context, subject string, data []byte) error
	// PublishMsg sends a full Message (including headers) to the stream.
	PublishMsg(ctx context.Context, msg *Message) error
	// Close tears down the producer connection.
	Close() error
}

// ---------------------------------------------------------------------------
// Consumer interface
// ---------------------------------------------------------------------------

// Consumer subscribes to stream subjects and delivers messages to handlers.
type Consumer interface {
	// Subscribe registers a handler for the given subject pattern.
	Subscribe(subject string, handler MessageHandler) error
	// Unsubscribe stops all active subscriptions.
	Unsubscribe() error
	// Close tears down the consumer connection.
	Close() error
}

// ---------------------------------------------------------------------------
// NATSProducer
// ---------------------------------------------------------------------------

// NATSProducer publishes messages to NATS JetStream.
type NATSProducer struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	config StreamConfig
}

// NewNATSProducer connects to NATS and ensures the target JetStream stream
// exists, creating or updating it as necessary.
func NewNATSProducer(cfg StreamConfig) (*NATSProducer, error) {
	nc, err := nats.Connect(cfg.URL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("stream: nats connect: %w", err)
	}

	js, err := nc.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("stream: jetstream context: %w", err)
	}

	if err := ensureStream(js, cfg); err != nil {
		nc.Close()
		return nil, err
	}

	return &NATSProducer{
		conn:   nc,
		js:     js,
		config: cfg,
	}, nil
}

// Publish sends raw bytes to the specified subject via JetStream.
func (p *NATSProducer) Publish(ctx context.Context, subject string, data []byte) error {
	msg := &Message{
		ID:        uuid.New().String(),
		Subject:   subject,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}
	return p.PublishMsg(ctx, msg)
}

// PublishMsg sends a fully constructed Message (with headers) via JetStream.
func (p *NATSProducer) PublishMsg(ctx context.Context, msg *Message) error {
	natsMsg := &nats.Msg{
		Subject: msg.Subject,
		Data:    msg.Data,
		Header:  nats.Header{},
	}

	natsMsg.Header.Set("Nats-Msg-Id", msg.ID)
	natsMsg.Header.Set("X-Timestamp", msg.Timestamp.Format(time.RFC3339Nano))

	for k, v := range msg.Headers {
		natsMsg.Header.Set(k, v)
	}

	_, err := p.js.PublishMsg(natsMsg, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("stream: publish to %s: %w", msg.Subject, err)
	}
	return nil
}

// Close drains and closes the underlying NATS connection.
func (p *NATSProducer) Close() error {
	if p.conn != nil {
		return p.conn.Drain()
	}
	return nil
}

// ---------------------------------------------------------------------------
// NATSConsumer
// ---------------------------------------------------------------------------

// NATSConsumer subscribes to JetStream subjects and dispatches messages to
// registered handlers.
type NATSConsumer struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	config StreamConfig

	mu   sync.Mutex
	subs []*nats.Subscription
}

// NewNATSConsumer connects to NATS and prepares a JetStream consumer. The
// stream must already exist (call NewNATSProducer first, or ensure it via
// other means).
func NewNATSConsumer(cfg StreamConfig) (*NATSConsumer, error) {
	nc, err := nats.Connect(cfg.URL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("stream: nats connect: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("stream: jetstream context: %w", err)
	}

	if err := ensureStream(js, cfg); err != nil {
		nc.Close()
		return nil, err
	}

	return &NATSConsumer{
		conn:   nc,
		js:     js,
		config: cfg,
	}, nil
}

// Subscribe creates a durable push subscription for the given subject. Each
// received message is converted to a *Message and passed to handler. If the
// handler returns nil the message is Ack'd; otherwise it is Nak'd for
// redelivery.
func (c *NATSConsumer) Subscribe(subject string, handler MessageHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	durableName := fmt.Sprintf("%s_%s", c.config.StreamName, sanitizeSubject(subject))

	sub, err := c.js.Subscribe(subject, func(natsMsg *nats.Msg) {
		msg := natsToMessage(natsMsg)

		if herr := handler(msg); herr != nil {
			// Negative acknowledge — the server will redeliver.
			_ = natsMsg.Nak()
			return
		}
		_ = natsMsg.Ack()
	},
		nats.Durable(durableName),
		nats.AckWait(c.config.AckWait),
		nats.MaxDeliver(c.config.MaxRetries),
		nats.ManualAck(),
	)
	if err != nil {
		return fmt.Errorf("stream: subscribe %s: %w", subject, err)
	}

	c.subs = append(c.subs, sub)
	return nil
}

// Unsubscribe drains and removes all active subscriptions.
func (c *NATSConsumer) Unsubscribe() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error
	for _, sub := range c.subs {
		if err := sub.Drain(); err != nil {
			errs = append(errs, err)
		}
	}
	c.subs = nil
	return errors.Join(errs...)
}

// Close unsubscribes everything and closes the NATS connection.
func (c *NATSConsumer) Close() error {
	if err := c.Unsubscribe(); err != nil {
		return err
	}
	if c.conn != nil {
		return c.conn.Drain()
	}
	return nil
}

// ---------------------------------------------------------------------------
// InMemoryStream — testing implementation
// ---------------------------------------------------------------------------

// InMemoryStream is a simple channel-based stream for unit testing that
// implements both Producer and Consumer.
type InMemoryStream struct {
	mu       sync.RWMutex
	subs     map[string][]MessageHandler
	messages []*Message
	closed   bool
}

// NewInMemoryStream creates an InMemoryStream ready for use.
func NewInMemoryStream() *InMemoryStream {
	return &InMemoryStream{
		subs: make(map[string][]MessageHandler),
	}
}

// Publish stores the message and fans it out to all matching subscribers.
func (s *InMemoryStream) Publish(_ context.Context, subject string, data []byte) error {
	msg := &Message{
		ID:        uuid.New().String(),
		Subject:   subject,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}
	return s.PublishMsg(context.Background(), msg)
}

// PublishMsg stores the message and fans it out to all matching subscribers.
func (s *InMemoryStream) PublishMsg(_ context.Context, msg *Message) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("stream: closed")
	}
	s.messages = append(s.messages, msg)

	// Collect matching handlers while holding the lock.
	var handlers []MessageHandler
	for pattern, hs := range s.subs {
		if matchSubject(pattern, msg.Subject) {
			handlers = append(handlers, hs...)
		}
	}
	s.mu.Unlock()

	for _, h := range handlers {
		// Ignore handler errors in the in-memory implementation (fire-and-forget).
		_ = h(msg)
	}
	return nil
}

// Subscribe registers a handler for the subject pattern. Patterns support
// NATS-style wildcards (* and >).
func (s *InMemoryStream) Subscribe(subject string, handler MessageHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("stream: closed")
	}
	s.subs[subject] = append(s.subs[subject], handler)
	return nil
}

// Unsubscribe removes all subscriptions.
func (s *InMemoryStream) Unsubscribe() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = make(map[string][]MessageHandler)
	return nil
}

// Close marks the stream as closed and removes all subscriptions.
func (s *InMemoryStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.subs = make(map[string][]MessageHandler)
	return nil
}

// Messages returns a snapshot of all published messages (useful in tests).
func (s *InMemoryStream) Messages() []*Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ensureStream creates or updates the JetStream stream described by cfg.
func ensureStream(js nats.JetStreamContext, cfg StreamConfig) error {
	streamCfg := &nats.StreamConfig{
		Name:      cfg.StreamName,
		Subjects:  cfg.Subjects,
		Retention: nats.LimitsPolicy,
		MaxAge:    72 * time.Hour,
		Storage:   nats.FileStorage,
		Replicas:  1,
	}

	_, err := js.StreamInfo(cfg.StreamName)
	if err != nil {
		// Stream does not exist — create it.
		if _, cerr := js.AddStream(streamCfg); cerr != nil {
			return fmt.Errorf("stream: create stream %s: %w", cfg.StreamName, cerr)
		}
		return nil
	}

	// Stream exists — update subjects if needed.
	if _, uerr := js.UpdateStream(streamCfg); uerr != nil {
		return fmt.Errorf("stream: update stream %s: %w", cfg.StreamName, uerr)
	}
	return nil
}

// natsToMessage converts a raw *nats.Msg to a *Message.
func natsToMessage(m *nats.Msg) *Message {
	msg := &Message{
		ID:      m.Header.Get("Nats-Msg-Id"),
		Subject: m.Subject,
		Data:    m.Data,
		Headers: make(map[string]string),
	}

	if ts := m.Header.Get("X-Timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			msg.Timestamp = t
		}
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}

	// Copy all headers except internal ones.
	for k, vals := range m.Header {
		if len(vals) > 0 {
			msg.Headers[k] = vals[0]
		}
	}

	// Extract redelivery metadata from JetStream metadata if available.
	meta, err := m.Metadata()
	if err == nil && meta != nil {
		msg.RedeliveryCount = int(meta.NumDelivered) - 1
	}

	return msg
}

// sanitizeSubject replaces characters unsuitable for NATS durable names.
func sanitizeSubject(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '*' || c == '>' || c == ' ' {
			out = append(out, '_')
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}

// matchSubject performs NATS-style pattern matching:
//   - '*' matches exactly one token (delimited by '.')
//   - '>' as the last token matches one or more tokens
//   - literal tokens must match exactly
func matchSubject(pattern, subject string) bool {
	pTokens := splitTokens(pattern)
	sTokens := splitTokens(subject)

	pi, si := 0, 0
	for pi < len(pTokens) && si < len(sTokens) {
		pt := pTokens[pi]
		switch pt {
		case ">":
			// '>' must be the last token and matches everything remaining.
			if pi == len(pTokens)-1 {
				return true
			}
			return false
		case "*":
			pi++
			si++
		default:
			if pt != sTokens[si] {
				return false
			}
			pi++
			si++
		}
	}
	return pi == len(pTokens) && si == len(sTokens)
}

// splitTokens splits a dot-delimited subject into tokens.
func splitTokens(s string) []string {
	if s == "" {
		return nil
	}
	tokens := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			tokens = append(tokens, s[start:i])
			start = i + 1
		}
	}
	tokens = append(tokens, s[start:])
	return tokens
}
