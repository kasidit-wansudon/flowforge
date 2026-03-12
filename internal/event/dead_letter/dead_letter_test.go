package deadletter_test

import (
	"fmt"
	"testing"
	"time"

	deadletter "github.com/kasidit-wansudon/flowforge/internal/event/dead_letter"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newDLQ(maxRetries int, maxAge time.Duration) *deadletter.InMemoryDLQ {
	return deadletter.NewInMemoryDLQ(deadletter.Config{
		MaxRetries: maxRetries,
		MaxAge:     maxAge,
	})
}

func sampleEntry(subject string) *deadletter.DeadLetter {
	return &deadletter.DeadLetter{
		OriginalMessage: []byte("payload"),
		Subject:         subject,
		Error:           "processing failed",
	}
}

// ---------------------------------------------------------------------------
// Push
// ---------------------------------------------------------------------------

func TestPush_AddsEntryToQueue(t *testing.T) {
	q := newDLQ(3, 0)
	dl := sampleEntry("flow.event")
	if err := q.Push(dl); err != nil {
		t.Fatalf("push: %v", err)
	}
	if q.Len() != 1 {
		t.Errorf("len = %d, want 1", q.Len())
	}
}

func TestPush_NilEntryFails(t *testing.T) {
	q := newDLQ(3, 0)
	if err := q.Push(nil); err == nil {
		t.Fatal("expected error when pushing nil entry")
	}
}

func TestPush_AutoAssignsID(t *testing.T) {
	q := newDLQ(3, 0)
	dl := sampleEntry("a.b")
	// Ensure ID is empty before push.
	dl.ID = ""
	_ = q.Push(dl)

	if dl.ID == "" {
		t.Error("expected ID to be auto-assigned by Push")
	}
}

func TestPush_SetsFirstFailedAtIfZero(t *testing.T) {
	q := newDLQ(3, 0)
	dl := sampleEntry("x.y")
	_ = q.Push(dl)
	if dl.FirstFailedAt.IsZero() {
		t.Error("expected FirstFailedAt to be set by Push")
	}
}

func TestPush_SetsAttemptsToOneIfZero(t *testing.T) {
	q := newDLQ(3, 0)
	dl := sampleEntry("x.y")
	dl.Attempts = 0
	_ = q.Push(dl)
	if dl.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", dl.Attempts)
	}
}

func TestPush_MultipleEntries(t *testing.T) {
	q := newDLQ(3, 0)
	for i := 0; i < 5; i++ {
		if err := q.Push(sampleEntry(fmt.Sprintf("s.%d", i))); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	if q.Len() != 5 {
		t.Errorf("len = %d, want 5", q.Len())
	}
}

// ---------------------------------------------------------------------------
// Pop
// ---------------------------------------------------------------------------

func TestPop_ReturnsAndRemovesOldestEntry(t *testing.T) {
	q := newDLQ(3, 0)
	_ = q.Push(sampleEntry("first"))
	_ = q.Push(sampleEntry("second"))

	dl, err := q.Pop()
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if dl == nil {
		t.Fatal("expected entry, got nil")
	}
	if dl.Subject != "first" {
		t.Errorf("subject = %q, want %q", dl.Subject, "first")
	}
	if q.Len() != 1 {
		t.Errorf("len after pop = %d, want 1", q.Len())
	}
}

func TestPop_EmptyQueueReturnsNil(t *testing.T) {
	q := newDLQ(3, 0)
	dl, err := q.Pop()
	if err != nil {
		t.Fatalf("pop on empty: %v", err)
	}
	if dl != nil {
		t.Errorf("expected nil from empty queue, got %+v", dl)
	}
}

// ---------------------------------------------------------------------------
// Peek
// ---------------------------------------------------------------------------

func TestPeek_DoesNotRemoveEntry(t *testing.T) {
	q := newDLQ(3, 0)
	_ = q.Push(sampleEntry("peek.me"))

	dl, err := q.Peek()
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if dl == nil {
		t.Fatal("expected entry from peek")
	}
	if q.Len() != 1 {
		t.Errorf("len after peek = %d, want 1 (peek must not remove)", q.Len())
	}
}

func TestPeek_EmptyQueueReturnsNil(t *testing.T) {
	q := newDLQ(3, 0)
	dl, err := q.Peek()
	if err != nil {
		t.Fatalf("peek on empty: %v", err)
	}
	if dl != nil {
		t.Errorf("expected nil, got %+v", dl)
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestGet_ReturnsEntryByID(t *testing.T) {
	q := newDLQ(3, 0)
	dl := sampleEntry("get.me")
	_ = q.Push(dl)

	got, err := q.Get(dl.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != dl.ID {
		t.Errorf("id = %q, want %q", got.ID, dl.ID)
	}
}

func TestGet_MissingIDFails(t *testing.T) {
	q := newDLQ(3, 0)
	_, err := q.Get("no-such-id")
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestList_ReturnsAllEntries(t *testing.T) {
	q := newDLQ(3, 0)
	for i := 0; i < 3; i++ {
		_ = q.Push(sampleEntry(fmt.Sprintf("list.%d", i)))
	}
	entries := q.List()
	if len(entries) != 3 {
		t.Errorf("list count = %d, want 3", len(entries))
	}
}

func TestList_IsSnapshotNotLiveReference(t *testing.T) {
	q := newDLQ(3, 0)
	_ = q.Push(sampleEntry("snap"))
	snap := q.List()

	// Add another entry after taking snapshot.
	_ = q.Push(sampleEntry("snap2"))

	if len(snap) != 1 {
		t.Errorf("snapshot should still have 1 entry, got %d", len(snap))
	}
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestRemove_DeletesEntry(t *testing.T) {
	q := newDLQ(3, 0)
	dl := sampleEntry("rm.me")
	_ = q.Push(dl)

	if err := q.Remove(dl.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if q.Len() != 0 {
		t.Errorf("len after remove = %d, want 0", q.Len())
	}
}

func TestRemove_MissingIDFails(t *testing.T) {
	q := newDLQ(3, 0)
	if err := q.Remove("not-here"); err == nil {
		t.Fatal("expected error when removing missing entry")
	}
}

// ---------------------------------------------------------------------------
// Retry
// ---------------------------------------------------------------------------

func TestRetry_IncrementsAttempts(t *testing.T) {
	q := newDLQ(5, 0)
	dl := sampleEntry("retry.me")
	_ = q.Push(dl)

	retried, err := q.Retry(dl.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retried.Attempts != 2 {
		t.Errorf("Attempts after retry = %d, want 2", retried.Attempts)
	}
}

func TestRetry_RemovesEntryFromQueue(t *testing.T) {
	q := newDLQ(5, 0)
	dl := sampleEntry("retry.remove")
	_ = q.Push(dl)

	_, err := q.Retry(dl.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	// After retry the item is removed so caller can re-publish.
	if q.Len() != 0 {
		t.Errorf("len after retry = %d, want 0", q.Len())
	}
}

func TestRetry_MissingIDFails(t *testing.T) {
	q := newDLQ(5, 0)
	_, err := q.Retry("no-such-id")
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestRetry_MaxRetriesExceededReturnsError(t *testing.T) {
	q := newDLQ(2, 0)
	dl := sampleEntry("exhaust")
	_ = q.Push(dl) // Attempts = 1

	// First retry: Attempts becomes 2 — still within limit.
	_, err := q.Retry(dl.ID)
	if err != nil {
		t.Fatalf("unexpected error on first retry: %v", err)
	}

	// Push again to simulate re-queue after first retry.
	dl2 := &deadletter.DeadLetter{
		ID:              dl.ID,
		OriginalMessage: dl.OriginalMessage,
		Subject:         dl.Subject,
		Error:           dl.Error,
		Attempts:        2, // carry over previous attempt count
	}
	_ = q.Push(dl2)

	// Second retry: Attempts becomes 3, exceeds MaxRetries=2.
	_, err = q.Retry(dl2.ID)
	if err == nil {
		t.Fatal("expected error when retries are exhausted")
	}
	if q.Len() != 0 {
		t.Errorf("entry should be removed after exhausting retries, len = %d", q.Len())
	}
}

// ---------------------------------------------------------------------------
// Expiry and Purge
// ---------------------------------------------------------------------------

func TestExpired_ReturnsFalseForZeroMaxAge(t *testing.T) {
	dl := &deadletter.DeadLetter{
		FirstFailedAt: time.Now().Add(-200 * time.Hour),
	}
	if dl.Expired(0) {
		t.Error("zero maxAge should mean never expires")
	}
}

func TestExpired_ReturnsTrueWhenOldEnough(t *testing.T) {
	dl := &deadletter.DeadLetter{
		FirstFailedAt: time.Now().Add(-2 * time.Hour),
	}
	if !dl.Expired(time.Hour) {
		t.Error("entry older than maxAge should be expired")
	}
}

func TestExpired_ReturnsFalseWhenNotOldEnough(t *testing.T) {
	dl := &deadletter.DeadLetter{
		FirstFailedAt: time.Now().Add(-30 * time.Minute),
	}
	if dl.Expired(time.Hour) {
		t.Error("entry younger than maxAge should not be expired")
	}
}

func TestPurge_RemovesExpiredEntries(t *testing.T) {
	q := newDLQ(3, time.Hour)

	// Add an old (expired) entry by hand-crafting FirstFailedAt.
	old := &deadletter.DeadLetter{
		Subject:         "old",
		OriginalMessage: []byte("x"),
		FirstFailedAt:   time.Now().Add(-2 * time.Hour),
		Attempts:        1,
	}
	_ = q.Push(old)

	// Add a fresh entry.
	fresh := sampleEntry("fresh")
	_ = q.Push(fresh)

	purged := q.Purge()
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}
	if q.Len() != 1 {
		t.Errorf("len after purge = %d, want 1", q.Len())
	}
}

func TestPurge_ZeroMaxAgeNeverPurges(t *testing.T) {
	q := newDLQ(3, 0) // 0 means never expire

	old := &deadletter.DeadLetter{
		Subject:         "old",
		OriginalMessage: []byte("x"),
		FirstFailedAt:   time.Now().Add(-1000 * time.Hour),
		Attempts:        1,
	}
	_ = q.Push(old)

	purged := q.Purge()
	if purged != 0 {
		t.Errorf("purged = %d, want 0 (zero MaxAge = no expiry)", purged)
	}
	if q.Len() != 1 {
		t.Errorf("len = %d, want 1", q.Len())
	}
}

func TestPurge_EmptyQueueReturnsZero(t *testing.T) {
	q := newDLQ(3, time.Hour)
	if n := q.Purge(); n != 0 {
		t.Errorf("purge on empty queue = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig_ReasonableValues(t *testing.T) {
	cfg := deadletter.DefaultConfig()
	if cfg.MaxRetries <= 0 {
		t.Errorf("MaxRetries = %d, want > 0", cfg.MaxRetries)
	}
	if cfg.MaxAge <= 0 {
		t.Errorf("MaxAge = %v, want > 0", cfg.MaxAge)
	}
}
