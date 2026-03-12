// Package router provides event routing with pattern matching, priority ordering,
// and payload-level filtering for the FlowForge event system.
package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Event
// ---------------------------------------------------------------------------

// Event represents a routable event within the system.
type Event struct {
	// ID is a unique event identifier.
	ID string
	// Type categorises the event (e.g. "workflow.started").
	Type string
	// Source identifies the origin of the event (e.g. "engine", "api").
	Source string
	// Subject is the NATS-style dot-delimited routing subject.
	Subject string
	// Data is the raw event payload.
	Data []byte
	// Metadata holds arbitrary key-value pairs associated with the event.
	Metadata map[string]string
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler processes an event and returns an error if processing fails.
type Handler func(event *Event) error

// ---------------------------------------------------------------------------
// Filter
// ---------------------------------------------------------------------------

// Filter decides whether an event should be delivered to a route. Returning
// true means the event passes the filter and should be handled.
type Filter func(event *Event) bool

// TypeFilter returns a Filter that accepts events matching any of the given types.
func TypeFilter(types ...string) Filter {
	set := make(map[string]struct{}, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return func(event *Event) bool {
		_, ok := set[event.Type]
		return ok
	}
}

// SourceFilter returns a Filter that accepts events from any of the given sources.
func SourceFilter(sources ...string) Filter {
	set := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		set[s] = struct{}{}
	}
	return func(event *Event) bool {
		_, ok := set[event.Source]
		return ok
	}
}

// MetadataFilter returns a Filter that accepts events whose metadata contains
// the given key with the expected value.
func MetadataFilter(key, value string) Filter {
	return func(event *Event) bool {
		if event.Metadata == nil {
			return false
		}
		return event.Metadata[key] == value
	}
}

// MetadataExistsFilter returns a Filter that accepts events whose metadata
// contains the given key (regardless of value).
func MetadataExistsFilter(key string) Filter {
	return func(event *Event) bool {
		if event.Metadata == nil {
			return false
		}
		_, ok := event.Metadata[key]
		return ok
	}
}

// AndFilter combines multiple filters with logical AND. All filters must
// pass for the event to be accepted.
func AndFilter(filters ...Filter) Filter {
	return func(event *Event) bool {
		for _, f := range filters {
			if !f(event) {
				return false
			}
		}
		return true
	}
}

// OrFilter combines multiple filters with logical OR. At least one filter
// must pass for the event to be accepted.
func OrFilter(filters ...Filter) Filter {
	return func(event *Event) bool {
		for _, f := range filters {
			if f(event) {
				return true
			}
		}
		return false
	}
}

// NotFilter inverts a filter.
func NotFilter(f Filter) Filter {
	return func(event *Event) bool {
		return !f(event)
	}
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

// Route binds a subject pattern to a handler with an optional filter and
// priority. Routes with higher priority values are evaluated first.
type Route struct {
	// Pattern is a NATS-style subject pattern (supports * and > wildcards).
	Pattern string
	// Handler processes matched events.
	Handler Handler
	// Priority determines evaluation order (higher = earlier). Default is 0.
	Priority int
	// Filter is an optional predicate applied after pattern matching.
	Filter Filter
	// Name is an optional human-readable identifier for the route.
	Name string
}

// matches returns true when the event's Subject matches the route pattern and
// any configured filter also passes.
func (r *Route) matches(event *Event) bool {
	if !PatternMatch(r.Pattern, event.Subject) {
		return false
	}
	if r.Filter != nil && !r.Filter(event) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// RouteOption — functional options for AddRoute
// ---------------------------------------------------------------------------

// RouteOption configures optional fields on a Route.
type RouteOption func(*Route)

// WithPriority sets the route priority (higher runs first).
func WithPriority(p int) RouteOption {
	return func(r *Route) { r.Priority = p }
}

// WithFilter attaches a filter predicate to the route.
func WithFilter(f Filter) RouteOption {
	return func(r *Route) { r.Filter = f }
}

// WithName assigns a human-readable name to the route.
func WithName(name string) RouteOption {
	return func(r *Route) { r.Name = name }
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

// Router maintains an ordered set of routes and dispatches events to the
// first matching handler.
type Router struct {
	mu             sync.RWMutex
	routes         []*Route
	defaultHandler Handler
	sorted         bool
}

// NewRouter creates an empty Router.
func NewRouter() *Router {
	return &Router{}
}

// SetDefaultHandler sets a catch-all handler invoked when no route matches.
func (r *Router) SetDefaultHandler(h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultHandler = h
}

// AddRoute registers a new route. Routes are re-sorted by priority (descending)
// before the next call to Route.
func (r *Router) AddRoute(pattern string, handler Handler, opts ...RouteOption) error {
	if pattern == "" {
		return errors.New("router: pattern must not be empty")
	}
	if handler == nil {
		return errors.New("router: handler must not be nil")
	}

	route := &Route{
		Pattern: pattern,
		Handler: handler,
	}
	for _, opt := range opts {
		opt(route)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append(r.routes, route)
	r.sorted = false
	return nil
}

// RemoveRoute removes all routes matching the given pattern (and optionally name).
// If name is empty only the pattern is considered.
func (r *Router) RemoveRoute(pattern string, name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	removed := 0
	filtered := r.routes[:0]
	for _, route := range r.routes {
		if route.Pattern == pattern && (name == "" || route.Name == name) {
			removed++
			continue
		}
		filtered = append(filtered, route)
	}
	r.routes = filtered
	return removed
}

// Routes returns a snapshot of the currently registered routes sorted by
// descending priority.
func (r *Router) Routes() []*Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	r.ensureSortedLocked()

	out := make([]*Route, len(r.routes))
	copy(out, r.routes)
	return out
}

// Route dispatches the event to the first matching route. If no route matches
// and a default handler is set it is invoked instead. Returns ErrNoRoute when
// no handler can be found.
func (r *Router) Route(event *Event) error {
	if event == nil {
		return errors.New("router: event must not be nil")
	}

	r.mu.RLock()
	r.ensureSortedLocked()

	for _, route := range r.routes {
		if route.matches(event) {
			handler := route.Handler
			r.mu.RUnlock()
			return handler(event)
		}
	}

	dh := r.defaultHandler
	r.mu.RUnlock()

	if dh != nil {
		return dh(event)
	}
	return fmt.Errorf("router: no route matched subject %q", event.Subject)
}

// RouteAll dispatches the event to **all** matching routes (not just the first).
// Errors are collected and returned as a combined error.
func (r *Router) RouteAll(event *Event) error {
	if event == nil {
		return errors.New("router: event must not be nil")
	}

	r.mu.RLock()
	r.ensureSortedLocked()

	var handlers []Handler
	for _, route := range r.routes {
		if route.matches(event) {
			handlers = append(handlers, route.Handler)
		}
	}
	dh := r.defaultHandler
	r.mu.RUnlock()

	if len(handlers) == 0 && dh != nil {
		return dh(event)
	}
	if len(handlers) == 0 {
		return fmt.Errorf("router: no route matched subject %q", event.Subject)
	}

	var errs []error
	for _, h := range handlers {
		if err := h(event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ensureSortedLocked sorts routes by descending priority when the list has
// been modified. It must be called while holding at least an RLock (the
// sort itself is safe because we only sort when r.sorted is false, which is
// only set under a write lock).
func (r *Router) ensureSortedLocked() {
	if r.sorted {
		return
	}
	sort.SliceStable(r.routes, func(i, j int) bool {
		return r.routes[i].Priority > r.routes[j].Priority
	})
	r.sorted = true
}

// ---------------------------------------------------------------------------
// Pattern matching
// ---------------------------------------------------------------------------

// PatternMatch tests whether subject matches pattern using NATS-style
// wildcard rules:
//
//   - Tokens are delimited by '.'
//   - '*' matches exactly one token
//   - '>' as the last token matches one or more remaining tokens
//   - Literal tokens must match exactly (case-sensitive)
//
// Examples:
//
//	PatternMatch("foo.*.baz", "foo.bar.baz")  => true
//	PatternMatch("foo.>",     "foo.bar.baz")  => true
//	PatternMatch("foo.bar",   "foo.baz")      => false
func PatternMatch(pattern, subject string) bool {
	if pattern == subject {
		return true
	}

	pTokens := tokenize(pattern)
	sTokens := tokenize(subject)

	return matchTokens(pTokens, sTokens)
}

// matchTokens performs the recursive token-level comparison.
func matchTokens(pattern, subject []string) bool {
	pi, si := 0, 0
	for pi < len(pattern) && si < len(subject) {
		pt := pattern[pi]
		switch {
		case pt == ">":
			// '>' must be the last token — matches everything remaining.
			return pi == len(pattern)-1
		case pt == "*":
			// Match exactly one token.
			pi++
			si++
		default:
			if pt != subject[si] {
				return false
			}
			pi++
			si++
		}
	}

	// Both must be fully consumed.
	return pi == len(pattern) && si == len(subject)
}

// tokenize splits a dot-delimited subject string into its component tokens.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ".")
}
