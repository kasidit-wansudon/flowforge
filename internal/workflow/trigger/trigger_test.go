package trigger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/workflow/trigger"
)

// --- CronTrigger ---

func TestCronTrigger_Type(t *testing.T) {
	ct := trigger.NewCronTrigger("* * * * *", "")
	if ct.Type() != "cron" {
		t.Errorf("expected type %q, got %q", "cron", ct.Type())
	}
}

func TestCronTrigger_InvalidScheduleReturnsError(t *testing.T) {
	ct := trigger.NewCronTrigger("not-a-cron", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ct.Start(ctx, func(ctx context.Context, event trigger.TriggerEvent) {})
	if err == nil {
		t.Error("expected error for invalid cron schedule")
		_ = ct.Stop()
	}
}

func TestCronTrigger_InvalidTimezoneReturnsError(t *testing.T) {
	ct := trigger.NewCronTrigger("* * * * *", "Invalid/Zone")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ct.Start(ctx, func(ctx context.Context, event trigger.TriggerEvent) {})
	if err == nil {
		t.Error("expected error for invalid timezone")
		_ = ct.Stop()
	}
}

func TestCronTrigger_DoubleStartReturnsError(t *testing.T) {
	ct := trigger.NewCronTrigger("* * * * *", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	noopHandler := func(ctx context.Context, event trigger.TriggerEvent) {}

	if err := ct.Start(ctx, noopHandler); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer ct.Stop()

	if err := ct.Start(ctx, noopHandler); err == nil {
		t.Error("expected error on double Start")
	}
}

func TestCronTrigger_StopIdempotent(t *testing.T) {
	ct := trigger.NewCronTrigger("* * * * *", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ct.Start(ctx, func(ctx context.Context, event trigger.TriggerEvent) {}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := ct.Stop(); err != nil {
		t.Fatalf("first Stop failed: %v", err)
	}
	// Second stop should be a no-op.
	if err := ct.Stop(); err != nil {
		t.Errorf("second Stop failed: %v", err)
	}
}

func TestCronTrigger_ContextCancellationStops(t *testing.T) {
	ct := trigger.NewCronTrigger("* * * * *", "")
	ctx, cancel := context.WithCancel(context.Background())

	if err := ct.Start(ctx, func(ctx context.Context, event trigger.TriggerEvent) {}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel context; trigger should stop without error.
	cancel()
	time.Sleep(100 * time.Millisecond) // allow goroutine to run

	// A subsequent Stop should also succeed (idempotent).
	if err := ct.Stop(); err != nil {
		t.Errorf("Stop after context cancel failed: %v", err)
	}
}

// --- WebhookTrigger ---

func TestWebhookTrigger_Type(t *testing.T) {
	wt := trigger.NewWebhookTrigger("/hook", 0, "")
	if wt.Type() != "webhook" {
		t.Errorf("expected type %q, got %q", "webhook", wt.Type())
	}
}

func TestWebhookTrigger_PathNormalization(t *testing.T) {
	// Without leading slash.
	wt := trigger.NewWebhookTrigger("hook", 0, "")
	if wt.Path != "/hook" {
		t.Errorf("expected path %q, got %q", "/hook", wt.Path)
	}
}

func TestWebhookTrigger_FiresOnValidPost(t *testing.T) {
	var received atomic.Int32
	var gotEvent trigger.TriggerEvent

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wt := trigger.NewWebhookTrigger("/webhook", 0, "")
	handler := func(_ context.Context, event trigger.TriggerEvent) {
		gotEvent = event
		received.Add(1)
	}
	if err := wt.Start(ctx, handler); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer wt.Stop()

	addr := wt.Addr()
	body := `{"action":"push","ref":"main"}`
	resp, err := http.Post(fmt.Sprintf("http://%s/webhook", addr), "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}

	// Wait for handler to fire.
	deadline := time.Now().Add(2 * time.Second)
	for received.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if received.Load() == 0 {
		t.Fatal("handler was never called")
	}
	if gotEvent.Type != "webhook" {
		t.Errorf("expected event type %q, got %q", "webhook", gotEvent.Type)
	}
}

func TestWebhookTrigger_RejectsGetMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wt := trigger.NewWebhookTrigger("/hook", 0, "")
	if err := wt.Start(ctx, func(_ context.Context, _ trigger.TriggerEvent) {}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer wt.Stop()

	resp, err := http.Get(fmt.Sprintf("http://%s/hook", wt.Addr()))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestWebhookTrigger_SecretValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wt := trigger.NewWebhookTrigger("/secure", 0, "my-secret")
	if err := wt.Start(ctx, func(_ context.Context, _ trigger.TriggerEvent) {}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer wt.Stop()

	// Without secret — should be 401.
	resp, err := http.Post(fmt.Sprintf("http://%s/secure", wt.Addr()), "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST (no secret) failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without secret, got %d", resp.StatusCode)
	}

	// With correct secret — should be 202.
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/secure", wt.Addr()), bytes.NewBufferString(`{}`))
	req.Header.Set("X-Webhook-Secret", "my-secret")
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST (correct secret) failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 with correct secret, got %d", resp2.StatusCode)
	}
}

func TestWebhookTrigger_ResponseBodyIsJSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wt := trigger.NewWebhookTrigger("/resp", 0, "")
	if err := wt.Start(ctx, func(_ context.Context, _ trigger.TriggerEvent) {}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer wt.Stop()

	resp, err := http.Post(fmt.Sprintf("http://%s/resp", wt.Addr()), "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if result["status"] != "accepted" {
		t.Errorf("expected status=accepted, got %q", result["status"])
	}
}

// --- EventTrigger ---

func TestEventTrigger_Type(t *testing.T) {
	et := trigger.NewEventTrigger("test.*")
	if et.Type() != "event" {
		t.Errorf("expected type %q, got %q", "event", et.Type())
	}
}

func TestEventTrigger_MatchingEventDelivered(t *testing.T) {
	et := trigger.NewEventTrigger("notification.*")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var received []trigger.TriggerEvent

	if err := et.Start(ctx, func(_ context.Context, event trigger.TriggerEvent) {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer et.Stop()

	et.Emit(trigger.TriggerEvent{Type: "notification.slack", Payload: map[string]interface{}{"msg": "hi"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("event was not delivered")
	}
	if received[0].Type != "notification.slack" {
		t.Errorf("unexpected event type %q", received[0].Type)
	}
}

func TestEventTrigger_NonMatchingEventDropped(t *testing.T) {
	et := trigger.NewEventTrigger("ci.*")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int32
	if err := et.Start(ctx, func(_ context.Context, event trigger.TriggerEvent) {
		count.Add(1)
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer et.Stop()

	et.Emit(trigger.TriggerEvent{Type: "deploy.done"})
	time.Sleep(50 * time.Millisecond)

	if count.Load() != 0 {
		t.Errorf("non-matching event should not have been delivered, got %d", count.Load())
	}
}

func TestEventTrigger_EmitBeforeStartDoesNothing(t *testing.T) {
	et := trigger.NewEventTrigger("*")
	// Should not panic or block.
	et.Emit(trigger.TriggerEvent{Type: "any.event"})
}

func TestEventTrigger_StopPreventsDelivery(t *testing.T) {
	et := trigger.NewEventTrigger("*")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int32
	if err := et.Start(ctx, func(_ context.Context, event trigger.TriggerEvent) {
		count.Add(1)
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := et.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	// Emit after stop — should not be delivered.
	et.Emit(trigger.TriggerEvent{Type: "some.event"})
	time.Sleep(50 * time.Millisecond)

	if count.Load() != 0 {
		t.Errorf("event should not be delivered after Stop, got %d deliveries", count.Load())
	}
}

// --- ManualTrigger ---

func TestManualTrigger_Type(t *testing.T) {
	mt := trigger.NewManualTrigger("test manual")
	if mt.Type() != "manual" {
		t.Errorf("expected type %q, got %q", "manual", mt.Type())
	}
}

func TestManualTrigger_FireDeliversEvent(t *testing.T) {
	mt := trigger.NewManualTrigger("test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var events []trigger.TriggerEvent

	if err := mt.Start(ctx, func(_ context.Context, event trigger.TriggerEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer mt.Stop()

	if err := mt.Fire(map[string]interface{}{"user": "alice"}); err != nil {
		t.Fatalf("Fire failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatal("manual trigger event not delivered")
	}
	if events[0].Type != "manual" {
		t.Errorf("expected event type %q, got %q", "manual", events[0].Type)
	}
	if events[0].Payload["user"] != "alice" {
		t.Errorf("unexpected payload: %v", events[0].Payload)
	}
}

func TestManualTrigger_FireBeforeStartReturnsError(t *testing.T) {
	mt := trigger.NewManualTrigger("test")
	err := mt.Fire(map[string]interface{}{})
	if err == nil {
		t.Error("expected error firing before Start")
	}
}

// --- TriggerManager ---

func TestTriggerManager_RegisterAndGet(t *testing.T) {
	mgr := trigger.NewTriggerManager()
	mt := trigger.NewManualTrigger("m1")

	if err := mgr.Register("manual1", mt); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := mgr.Get("manual1")
	if !ok {
		t.Fatal("expected to find trigger 'manual1'")
	}
	if got.Type() != "manual" {
		t.Errorf("expected type %q, got %q", "manual", got.Type())
	}
}

func TestTriggerManager_RegisterDuplicateReturnsError(t *testing.T) {
	mgr := trigger.NewTriggerManager()
	mt := trigger.NewManualTrigger("m")

	if err := mgr.Register("t1", mt); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := mgr.Register("t1", trigger.NewManualTrigger("m2")); err == nil {
		t.Error("expected error registering duplicate trigger name")
	}
}

func TestTriggerManager_StartAndStopAll(t *testing.T) {
	mgr := trigger.NewTriggerManager()
	_ = mgr.Register("m1", trigger.NewManualTrigger("first"))
	_ = mgr.Register("m2", trigger.NewManualTrigger("second"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx, func(_ context.Context, _ trigger.TriggerEvent) {}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := mgr.StopAll(); err != nil {
		t.Fatalf("StopAll failed: %v", err)
	}
}

func TestTriggerManager_List(t *testing.T) {
	mgr := trigger.NewTriggerManager()
	_ = mgr.Register("alpha", trigger.NewManualTrigger("a"))
	_ = mgr.Register("beta", trigger.NewManualTrigger("b"))
	_ = mgr.Register("gamma", trigger.NewManualTrigger("c"))

	names := mgr.List()
	if len(names) != 3 {
		t.Errorf("expected 3 triggers, got %d", len(names))
	}
}

func TestTriggerManager_GetUnknownReturnsNotFound(t *testing.T) {
	mgr := trigger.NewTriggerManager()
	_, ok := mgr.Get("doesNotExist")
	if ok {
		t.Error("expected ok=false for unknown trigger")
	}
}
