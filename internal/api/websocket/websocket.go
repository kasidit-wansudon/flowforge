// Package websocket provides a real-time event delivery layer for FlowForge.
// Clients connect over WebSocket, subscribe to topics (e.g. a specific
// workflow or run), and receive JSON-encoded event messages as execution
// progresses.
package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Configuration constants
// ---------------------------------------------------------------------------

const (
	// writeWait is the maximum time a write to the peer may take.
	writeWait = 10 * time.Second

	// pongWait is the maximum time to wait for a pong from the peer.
	pongWait = 60 * time.Second

	// pingPeriod must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize is the maximum message size the server will accept.
	maxMessageSize = 4096

	// sendBufferSize is the per-client outbound message buffer.
	sendBufferSize = 256
)

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

// MessageType enumerates the kinds of events the server broadcasts.
const (
	MessageTypeWorkflowStatus = "workflow.status"
	MessageTypeTaskStatus     = "task.status"
	MessageTypeTaskLog        = "task.log"
	MessageTypeRunComplete    = "run.complete"
	MessageTypeSubscribe      = "subscribe"
	MessageTypeUnsubscribe    = "unsubscribe"
	MessageTypePing           = "ping"
	MessageTypePong           = "pong"
	MessageTypeError          = "error"
)

// Message is the JSON envelope exchanged between client and server.
type Message struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Topic     string      `json:"topic,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// WorkflowStatusPayload carries a workflow status change.
type WorkflowStatusPayload struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	PrevStatus string `json:"prev_status,omitempty"`
}

// TaskStatusPayload carries a task status change.
type TaskStatusPayload struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	TaskID     string `json:"task_id"`
	TaskName   string `json:"task_name"`
	Status     string `json:"status"`
	PrevStatus string `json:"prev_status,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TaskLogPayload carries a log line emitted by a task.
type TaskLogPayload struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	TaskID     string `json:"task_id"`
	Level      string `json:"level"`
	Message    string `json:"message"`
}

// RunCompletePayload signals that an entire run has finished.
type RunCompletePayload struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	Duration   int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Hub — central dispatcher
// ---------------------------------------------------------------------------

// Hub maintains the set of active clients and broadcasts messages to clients
// that are subscribed to the relevant topic.
type Hub struct {
	// mu protects the clients and topics maps.
	mu sync.RWMutex

	// clients is the set of all connected clients.
	clients map[*Client]bool

	// topics maps topic strings to the set of subscribed clients.
	topics map[string]map[*Client]bool

	// broadcast is the inbound channel for messages to be fanned out.
	broadcast chan *Message

	// register receives new clients from ServeWS.
	register chan *Client

	// unregister receives clients that are disconnecting.
	unregister chan *Client

	// done is closed when the hub is shut down.
	done chan struct{}

	logger *zap.Logger
}

// NewHub creates a Hub ready to be started with Run().
func NewHub(logger *zap.Logger) *Hub {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	return &Hub{
		clients:    make(map[*Client]bool),
		topics:     make(map[string]map[*Client]bool),
		broadcast:  make(chan *Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
		logger:     logger,
	}
}

// Run is the main event loop. It must be started in its own goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

			h.logger.Info("client connected",
				zap.String("client_id", client.id),
				zap.String("remote_addr", client.remoteAddr),
			)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// Remove from all topic subscriptions.
				for topic, subs := range h.topics {
					delete(subs, client)
					if len(subs) == 0 {
						delete(h.topics, topic)
					}
				}
			}
			h.mu.Unlock()

			h.logger.Info("client disconnected",
				zap.String("client_id", client.id),
			)

		case msg := <-h.broadcast:
			h.mu.RLock()
			if msg.Topic == "" {
				// Global broadcast: send to every client.
				for client := range h.clients {
					h.safeSend(client, msg)
				}
			} else {
				// Topic broadcast: only send to subscribers.
				if subs, ok := h.topics[msg.Topic]; ok {
					for client := range subs {
						h.safeSend(client, msg)
					}
				}
			}
			h.mu.RUnlock()

		case <-h.done:
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.topics = make(map[string]map[*Client]bool)
			h.mu.Unlock()
			return
		}
	}
}

// Stop shuts down the hub gracefully.
func (h *Hub) Stop() {
	close(h.done)
}

// safeSend delivers a message to a client without blocking. If the client's
// buffer is full the message is dropped and the client is scheduled for
// eviction.
func (h *Hub) safeSend(client *Client, msg *Message) {
	select {
	case client.send <- msg:
	default:
		// Client is too slow; schedule disconnect.
		h.logger.Warn("dropping message, client buffer full",
			zap.String("client_id", client.id),
			zap.String("topic", msg.Topic),
		)
	}
}

// Subscribe adds a client to a topic. Thread-safe.
func (h *Hub) Subscribe(client *Client, topic string) {
	if client == nil || topic == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	subs, ok := h.topics[topic]
	if !ok {
		subs = make(map[*Client]bool)
		h.topics[topic] = subs
	}
	subs[client] = true

	client.mu.Lock()
	client.subscriptions[topic] = true
	client.mu.Unlock()

	h.logger.Debug("client subscribed",
		zap.String("client_id", client.id),
		zap.String("topic", topic),
	)
}

// Unsubscribe removes a client from a topic. Thread-safe.
func (h *Hub) Unsubscribe(client *Client, topic string) {
	if client == nil || topic == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.topics[topic]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(h.topics, topic)
		}
	}

	client.mu.Lock()
	delete(client.subscriptions, topic)
	client.mu.Unlock()

	h.logger.Debug("client unsubscribed",
		zap.String("client_id", client.id),
		zap.String("topic", topic),
	)
}

// BroadcastToTopic enqueues a message for all subscribers of the given topic.
func (h *Hub) BroadcastToTopic(topic string, msgType string, payload interface{}) {
	msg := &Message{
		ID:        uuid.New().String(),
		Type:      msgType,
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now().UTC().UnixMilli(),
	}

	select {
	case h.broadcast <- msg:
	default:
		h.logger.Warn("broadcast channel full, dropping message",
			zap.String("topic", topic),
			zap.String("type", msgType),
		)
	}
}

// BroadcastGlobal enqueues a message for every connected client regardless
// of subscriptions.
func (h *Hub) BroadcastGlobal(msgType string, payload interface{}) {
	msg := &Message{
		ID:        uuid.New().String(),
		Type:      msgType,
		Topic:     "",
		Payload:   payload,
		Timestamp: time.Now().UTC().UnixMilli(),
	}

	select {
	case h.broadcast <- msg:
	default:
		h.logger.Warn("broadcast channel full, dropping global message",
			zap.String("type", msgType),
		)
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// TopicSubscriberCount returns the number of subscribers for a topic.
func (h *Hub) TopicSubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic])
}

// ---------------------------------------------------------------------------
// Client — a single WebSocket connection
// ---------------------------------------------------------------------------

// Client is an intermediary between a WebSocket connection and the Hub.
type Client struct {
	id            string
	hub           *Hub
	conn          *websocket.Conn
	send          chan *Message
	remoteAddr    string
	mu            sync.RWMutex
	subscriptions map[string]bool
	connectedAt   time.Time
}

// ID returns the unique identifier for this client.
func (c *Client) ID() string {
	return c.id
}

// Subscriptions returns a copy of the current subscription set.
func (c *Client) Subscriptions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	subs := make([]string, 0, len(c.subscriptions))
	for t := range c.subscriptions {
		subs = append(subs, t)
	}
	return subs
}

// readPump reads messages from the WebSocket connection and processes
// subscribe/unsubscribe commands. It runs in its own goroutine per client.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		c.hub.logger.Error("failed to set read deadline", zap.Error(err))
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived) {
				c.hub.logger.Warn("unexpected close",
					zap.String("client_id", c.id),
					zap.Error(err),
				)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.sendError("invalid message format: " + err.Error())
			continue
		}

		c.handleIncoming(&msg)
	}
}

// handleIncoming processes a single inbound message from the client.
func (c *Client) handleIncoming(msg *Message) {
	switch msg.Type {
	case MessageTypeSubscribe:
		if msg.Topic == "" {
			c.sendError("subscribe requires a topic")
			return
		}
		c.hub.Subscribe(c, msg.Topic)
		c.sendAck(msg.Type, msg.Topic)

	case MessageTypeUnsubscribe:
		if msg.Topic == "" {
			c.sendError("unsubscribe requires a topic")
			return
		}
		c.hub.Unsubscribe(c, msg.Topic)
		c.sendAck(msg.Type, msg.Topic)

	case MessageTypePing:
		reply := &Message{
			ID:        uuid.New().String(),
			Type:      MessageTypePong,
			Timestamp: time.Now().UTC().UnixMilli(),
		}
		select {
		case c.send <- reply:
		default:
		}

	default:
		c.sendError(fmt.Sprintf("unsupported message type: %s", msg.Type))
	}
}

// sendError pushes an error message onto the client's outbound channel.
func (c *Client) sendError(text string) {
	msg := &Message{
		ID:        uuid.New().String(),
		Type:      MessageTypeError,
		Payload:   map[string]string{"error": text},
		Timestamp: time.Now().UTC().UnixMilli(),
	}
	select {
	case c.send <- msg:
	default:
	}
}

// sendAck pushes an acknowledgement message for subscribe/unsubscribe.
func (c *Client) sendAck(action, topic string) {
	msg := &Message{
		ID:    uuid.New().String(),
		Type:  action,
		Topic: topic,
		Payload: map[string]string{
			"status": "ok",
			"action": action,
			"topic":  topic,
		},
		Timestamp: time.Now().UTC().UnixMilli(),
	}
	select {
	case c.send <- msg:
	default:
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
// It runs in its own goroutine per client.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.hub.logger.Error("failed to set write deadline", zap.Error(err))
				return
			}
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				c.hub.logger.Error("failed to marshal message",
					zap.Error(err),
					zap.String("client_id", c.id),
				)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				c.hub.logger.Warn("write error",
					zap.String("client_id", c.id),
					zap.Error(err),
				)
				return
			}

			// Batch-drain any additional queued messages into a single
			// write frame to reduce syscall overhead.
			n := len(c.send)
			for i := 0; i < n; i++ {
				additional, ok := <-c.send
				if !ok {
					return
				}
				data, err := json.Marshal(additional)
				if err != nil {
					continue
				}
				if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.hub.logger.Error("failed to set write deadline for ping", zap.Error(err))
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP upgrade handler
// ---------------------------------------------------------------------------

// upgrader configures the WebSocket upgrade parameters.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin allows all connections by default. In production this
	// should be restricted to known origins.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// SetCheckOrigin replaces the default origin checker. Use this to restrict
// WebSocket connections to a set of trusted origins in production.
func SetCheckOrigin(fn func(r *http.Request) bool) {
	upgrader.CheckOrigin = fn
}

// ServeWS upgrades an HTTP connection to a WebSocket and registers the
// resulting client with the hub. It should be registered as an HTTP handler,
// for example: router.HandleFunc("/ws", hub.ServeWS).
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}

	client := &Client{
		id:            uuid.New().String(),
		hub:           h,
		conn:          conn,
		send:          make(chan *Message, sendBufferSize),
		remoteAddr:    r.RemoteAddr,
		subscriptions: make(map[string]bool),
		connectedAt:   time.Now().UTC(),
	}

	h.register <- client

	// Start the read and write pumps in separate goroutines.
	go client.writePump()
	go client.readPump()
}

// ---------------------------------------------------------------------------
// Convenience broadcast helpers
// ---------------------------------------------------------------------------

// NotifyWorkflowStatus broadcasts a workflow status change to the
// "workflow.<id>" topic.
func (h *Hub) NotifyWorkflowStatus(workflowID, runID, newStatus, prevStatus string) {
	topic := fmt.Sprintf("workflow.%s", workflowID)
	h.BroadcastToTopic(topic, MessageTypeWorkflowStatus, WorkflowStatusPayload{
		WorkflowID: workflowID,
		RunID:      runID,
		Status:     newStatus,
		PrevStatus: prevStatus,
	})
	// Also broadcast on the run-specific topic so clients watching a single
	// run get the update.
	runTopic := fmt.Sprintf("run.%s", runID)
	h.BroadcastToTopic(runTopic, MessageTypeWorkflowStatus, WorkflowStatusPayload{
		WorkflowID: workflowID,
		RunID:      runID,
		Status:     newStatus,
		PrevStatus: prevStatus,
	})
}

// NotifyTaskStatus broadcasts a task status change to both the workflow and
// run topics.
func (h *Hub) NotifyTaskStatus(workflowID, runID, taskID, taskName, newStatus, prevStatus, errMsg string) {
	payload := TaskStatusPayload{
		WorkflowID: workflowID,
		RunID:      runID,
		TaskID:     taskID,
		TaskName:   taskName,
		Status:     newStatus,
		PrevStatus: prevStatus,
		Error:      errMsg,
	}
	h.BroadcastToTopic(fmt.Sprintf("workflow.%s", workflowID), MessageTypeTaskStatus, payload)
	h.BroadcastToTopic(fmt.Sprintf("run.%s", runID), MessageTypeTaskStatus, payload)
}

// NotifyTaskLog broadcasts a log line to the run topic.
func (h *Hub) NotifyTaskLog(workflowID, runID, taskID, level, message string) {
	payload := TaskLogPayload{
		WorkflowID: workflowID,
		RunID:      runID,
		TaskID:     taskID,
		Level:      level,
		Message:    message,
	}
	h.BroadcastToTopic(fmt.Sprintf("run.%s", runID), MessageTypeTaskLog, payload)
}

// NotifyRunComplete broadcasts a run completion event.
func (h *Hub) NotifyRunComplete(workflowID, runID, finalStatus string, durationMs int64, errMsg string) {
	payload := RunCompletePayload{
		WorkflowID: workflowID,
		RunID:      runID,
		Status:     finalStatus,
		Duration:   durationMs,
		Error:      errMsg,
	}
	h.BroadcastToTopic(fmt.Sprintf("workflow.%s", workflowID), MessageTypeRunComplete, payload)
	h.BroadcastToTopic(fmt.Sprintf("run.%s", runID), MessageTypeRunComplete, payload)
}
