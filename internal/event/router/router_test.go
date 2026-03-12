package router

import (
	"errors"
	"strings"
	"testing"
)

func TestPatternMatchExact(t *testing.T) {
	if !PatternMatch("foo.bar.baz", "foo.bar.baz") {
		t.Error("exact match should return true")
	}
	if PatternMatch("foo.bar", "foo.baz") {
		t.Error("different literals should not match")
	}
}

func TestPatternMatchSingleWildcard(t *testing.T) {
	if !PatternMatch("foo.*.baz", "foo.bar.baz") {
		t.Error("* should match single token")
	}
	if PatternMatch("foo.*.baz", "foo.bar.qux.baz") {
		t.Error("* should not match multiple tokens")
	}
}

func TestPatternMatchMultiWildcard(t *testing.T) {
	if !PatternMatch("foo.>", "foo.bar.baz") {
		t.Error("> should match one or more remaining tokens")
	}
	if !PatternMatch("foo.>", "foo.bar") {
		t.Error("> should match single remaining token")
	}
	if PatternMatch("foo.>", "foo") {
		t.Error("> requires at least one remaining token")
	}
}

func TestPatternMatchEmpty(t *testing.T) {
	if PatternMatch("", "foo") {
		t.Error("empty pattern should not match")
	}
	if PatternMatch("foo", "") {
		t.Error("should not match empty subject")
	}
}

func TestNewRouter(t *testing.T) {
	r := NewRouter()
	if r == nil {
		t.Fatal("expected non-nil router")
	}
}

func TestAddRouteAndRoute(t *testing.T) {
	r := NewRouter()
	var captured *Event
	err := r.AddRoute("workflow.>", func(event *Event) error {
		captured = event
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := &Event{ID: "e-1", Subject: "workflow.started"}
	if err := r.Route(event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured == nil || captured.ID != "e-1" {
		t.Error("handler should have received the event")
	}
}

func TestRouteNoMatch(t *testing.T) {
	r := NewRouter()
	_ = r.AddRoute("foo.bar", func(event *Event) error { return nil })

	err := r.Route(&Event{ID: "e-1", Subject: "baz.qux"})
	if err == nil {
		t.Fatal("expected no-route error")
	}
}

func TestRouteNilEvent(t *testing.T) {
	r := NewRouter()
	err := r.Route(nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
}

func TestAddRouteEmptyPattern(t *testing.T) {
	r := NewRouter()
	err := r.AddRoute("", func(event *Event) error { return nil })
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestAddRouteNilHandler(t *testing.T) {
	r := NewRouter()
	err := r.AddRoute("foo.bar", nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestRoutePriorityOrdering(t *testing.T) {
	r := NewRouter()
	var handledBy string

	_ = r.AddRoute("event.*", func(event *Event) error {
		handledBy = "low"
		return nil
	}, WithPriority(1), WithName("low"))

	_ = r.AddRoute("event.*", func(event *Event) error {
		handledBy = "high"
		return nil
	}, WithPriority(10), WithName("high"))

	_ = r.Route(&Event{Subject: "event.test"})
	if handledBy != "high" {
		t.Errorf("expected high priority handler, got %s", handledBy)
	}
}

func TestRouteAllMultipleHandlers(t *testing.T) {
	r := NewRouter()
	var calls []string

	_ = r.AddRoute("event.>", func(event *Event) error {
		calls = append(calls, "handler1")
		return nil
	}, WithPriority(10))

	_ = r.AddRoute("event.>", func(event *Event) error {
		calls = append(calls, "handler2")
		return nil
	}, WithPriority(5))

	err := r.RouteAll(&Event{Subject: "event.test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 handlers called, got %d", len(calls))
	}
}

func TestRouteAllCollectsErrors(t *testing.T) {
	r := NewRouter()
	_ = r.AddRoute("event.>", func(event *Event) error {
		return errors.New("err1")
	})
	_ = r.AddRoute("event.>", func(event *Event) error {
		return errors.New("err2")
	})

	err := r.RouteAll(&Event{Subject: "event.test"})
	if err == nil {
		t.Fatal("expected combined errors")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "err1") || !strings.Contains(errStr, "err2") {
		t.Errorf("expected both errors, got %s", errStr)
	}
}

func TestDefaultHandler(t *testing.T) {
	r := NewRouter()
	var defaultCalled bool
	r.SetDefaultHandler(func(event *Event) error {
		defaultCalled = true
		return nil
	})

	_ = r.Route(&Event{Subject: "unmatched.subject"})
	if !defaultCalled {
		t.Error("default handler should have been called")
	}
}

func TestTypeFilter(t *testing.T) {
	f := TypeFilter("workflow.started", "workflow.completed")
	if !f(&Event{Type: "workflow.started"}) {
		t.Error("should match workflow.started")
	}
	if f(&Event{Type: "workflow.failed"}) {
		t.Error("should not match workflow.failed")
	}
}

func TestSourceFilter(t *testing.T) {
	f := SourceFilter("engine")
	if !f(&Event{Source: "engine"}) {
		t.Error("should match engine source")
	}
	if f(&Event{Source: "api"}) {
		t.Error("should not match api source")
	}
}

func TestMetadataFilter(t *testing.T) {
	f := MetadataFilter("env", "prod")
	if !f(&Event{Metadata: map[string]string{"env": "prod"}}) {
		t.Error("should match metadata")
	}
	if f(&Event{Metadata: map[string]string{"env": "dev"}}) {
		t.Error("should not match different value")
	}
	if f(&Event{}) {
		t.Error("should not match nil metadata")
	}
}

func TestMetadataExistsFilter(t *testing.T) {
	f := MetadataExistsFilter("trace_id")
	if !f(&Event{Metadata: map[string]string{"trace_id": "123"}}) {
		t.Error("should match when key exists")
	}
	if f(&Event{Metadata: map[string]string{"other": "val"}}) {
		t.Error("should not match when key missing")
	}
}

func TestAndFilter(t *testing.T) {
	f := AndFilter(
		TypeFilter("workflow.started"),
		SourceFilter("engine"),
	)
	if !f(&Event{Type: "workflow.started", Source: "engine"}) {
		t.Error("should match when both conditions pass")
	}
	if f(&Event{Type: "workflow.started", Source: "api"}) {
		t.Error("should not match when one condition fails")
	}
}

func TestOrFilter(t *testing.T) {
	f := OrFilter(
		SourceFilter("engine"),
		SourceFilter("api"),
	)
	if !f(&Event{Source: "engine"}) {
		t.Error("should match engine")
	}
	if !f(&Event{Source: "api"}) {
		t.Error("should match api")
	}
	if f(&Event{Source: "external"}) {
		t.Error("should not match external")
	}
}

func TestNotFilter(t *testing.T) {
	f := NotFilter(SourceFilter("engine"))
	if f(&Event{Source: "engine"}) {
		t.Error("should not match engine")
	}
	if !f(&Event{Source: "api"}) {
		t.Error("should match non-engine")
	}
}

func TestRouteWithFilter(t *testing.T) {
	r := NewRouter()
	var handled bool
	_ = r.AddRoute("event.*", func(event *Event) error {
		handled = true
		return nil
	}, WithFilter(SourceFilter("engine")))

	// Should not trigger because source doesn't match filter
	_ = r.Route(&Event{Subject: "event.test", Source: "api"})
	if handled {
		t.Error("handler should not have been called with wrong source")
	}

	// Should trigger now
	_ = r.Route(&Event{Subject: "event.test", Source: "engine"})
	if !handled {
		t.Error("handler should have been called with matching source")
	}
}

func TestRemoveRoute(t *testing.T) {
	r := NewRouter()
	_ = r.AddRoute("foo.bar", func(event *Event) error { return nil }, WithName("test"))
	_ = r.AddRoute("foo.baz", func(event *Event) error { return nil }, WithName("other"))

	removed := r.RemoveRoute("foo.bar", "test")
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	routes := r.Routes()
	if len(routes) != 1 {
		t.Errorf("expected 1 remaining route, got %d", len(routes))
	}
}

func TestRouteAllNilEvent(t *testing.T) {
	r := NewRouter()
	err := r.RouteAll(nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
}
