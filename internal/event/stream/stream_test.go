package stream_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/event/stream"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newStream() *stream.InMemoryStream {
	return stream.NewInMemoryStream()
}

// collect subscribes to subject and returns a channel that receives all
// messages delivered to that subscription.
func collect(t *testing.T, s *stream.InMemoryStream, subject string) <-chan *stream.Message {
	t.Helper()
	ch := make(chan *stream.Message, 16)
	err := s.Subscribe(subject, func(msg *stream.Message) error {
		ch <- msg
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe %q: %v", subject, err)
	}
	return ch
}

// waitForN waits up to timeout for n messages to arrive on ch.
func waitForN(t *testing.T, ch <-chan *stream.Message, n int, timeout time.Duration) []*stream.Message {
	t.Helper()
	var msgs []*stream.Message
	deadline := time.After(timeout)
	for len(msgs) < n {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		case <-deadline:
			t.Fatalf("timed out waiting for %d messages; got %d", n, len(msgs))
		}
	}
	return msgs
}

// ---------------------------------------------------------------------------
// Basic publish / subscribe
// ---------------------------------------------------------------------------

func TestPublish_DeliversToBoundSubscriber(t *testing.T) {
	s := newStream()
	ch := collect(t, s, "test.event")

	err := s.Publish(context.Background(), "test.event", []byte("hello"))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	msgs := waitForN(t, ch, 1, time.Second)
	if string(msgs[0].Data) != "hello" {
		t.Errorf("data = %q, want %q", msgs[0].Data, "hello")
	}
	if msgs[0].Subject != "test.event" {
		t.Errorf("subject = %q, want %q", msgs[0].Subject, "test.event")
	}
}

func TestPublish_AssignsUniqueIDs(t *testing.T) {
	s := newStream()
	ch := collect(t, s, "ids.test")

	_ = s.Publish(context.Background(), "ids.test", []byte("a"))
	_ = s.Publish(context.Background(), "ids.test", []byte("b"))

	msgs := waitForN(t, ch, 2, time.Second)
	if msgs[0].ID == msgs[1].ID {
		t.Errorf("two messages have the same ID: %q", msgs[0].ID)
	}
}

func TestPublishMsg_PreservesHeaders(t *testing.T) {
	s := newStream()
	ch := collect(t, s, "hdr.test")

	msg := &stream.Message{
		Subject: "hdr.test",
		Data:    []byte("payload"),
		Headers: map[string]string{"X-Custom": "myvalue"},
	}
	if err := s.PublishMsg(context.Background(), msg); err != nil {
		t.Fatalf("publishmsg: %v", err)
	}

	msgs := waitForN(t, ch, 1, time.Second)
	if msgs[0].Headers["X-Custom"] != "myvalue" {
		t.Errorf("header X-Custom = %q, want %q", msgs[0].Headers["X-Custom"], "myvalue")
	}
}

func TestPublish_ClosedStreamFails(t *testing.T) {
	s := newStream()
	s.Close()

	err := s.Publish(context.Background(), "any", []byte("x"))
	if err == nil {
		t.Fatal("expected error when publishing to closed stream")
	}
}

func TestSubscribe_ClosedStreamFails(t *testing.T) {
	s := newStream()
	s.Close()

	err := s.Subscribe("any", func(_ *stream.Message) error { return nil })
	if err == nil {
		t.Fatal("expected error when subscribing to closed stream")
	}
}

// ---------------------------------------------------------------------------
// Wildcard pattern matching
// ---------------------------------------------------------------------------

func TestWildcard_SingleTokenStar(t *testing.T) {
	s := newStream()
	// "events.*.created" should match "events.user.created"
	ch := collect(t, s, "events.*.created")

	_ = s.Publish(context.Background(), "events.user.created", []byte("u"))
	_ = s.Publish(context.Background(), "events.order.created", []byte("o"))
	// Should NOT match (wrong suffix).
	_ = s.Publish(context.Background(), "events.user.deleted", []byte("d"))

	msgs := waitForN(t, ch, 2, time.Second)
	for _, m := range msgs {
		if m.Subject == "events.user.deleted" {
			t.Error("wildcard * should not match 'deleted' suffix")
		}
	}
}

func TestWildcard_MultiTokenGreaterThan(t *testing.T) {
	s := newStream()
	// "flowforge.>" should match anything starting with "flowforge."
	ch := collect(t, s, "flowforge.>")

	_ = s.Publish(context.Background(), "flowforge.run.started", []byte("1"))
	_ = s.Publish(context.Background(), "flowforge.workflow.created", []byte("2"))
	// Should NOT match.
	_ = s.Publish(context.Background(), "other.subject", []byte("3"))

	msgs := waitForN(t, ch, 2, time.Second)
	for _, m := range msgs {
		if m.Subject == "other.subject" {
			t.Error("'>' wildcard should not match 'other.subject'")
		}
	}
}

func TestExactMatch_OnlyDeliversToMatchingSubject(t *testing.T) {
	s := newStream()
	chExact := collect(t, s, "exact.subject")
	chOther := collect(t, s, "other.subject")

	_ = s.Publish(context.Background(), "exact.subject", []byte("hit"))

	// exact subscriber must receive.
	waitForN(t, chExact, 1, time.Second)

	// other subscriber must not receive within a short window.
	select {
	case m := <-chOther:
		t.Errorf("unexpected delivery to other.subject: %v", m)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

// ---------------------------------------------------------------------------
// Multiple subscribers
// ---------------------------------------------------------------------------

func TestMultipleSubscribers_AllReceive(t *testing.T) {
	s := newStream()
	ch1 := collect(t, s, "fan.out")
	ch2 := collect(t, s, "fan.out")
	ch3 := collect(t, s, "fan.out")

	_ = s.Publish(context.Background(), "fan.out", []byte("broadcast"))

	waitForN(t, ch1, 1, time.Second)
	waitForN(t, ch2, 1, time.Second)
	waitForN(t, ch3, 1, time.Second)
}

func TestMultipleSubscribers_ConcurrentPublish(t *testing.T) {
	s := newStream()

	var received int64
	const workers = 10
	const msgsPerWorker = 5

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Subscribe("concurrent.test", func(_ *stream.Message) error {
				atomic.AddInt64(&received, 1)
				return nil
			})
		}()
	}
	wg.Wait()

	for i := 0; i < msgsPerWorker; i++ {
		_ = s.Publish(context.Background(), "concurrent.test", []byte("data"))
	}

	// Allow time for all callbacks to fire.
	time.Sleep(50 * time.Millisecond)

	got := atomic.LoadInt64(&received)
	if got != int64(workers*msgsPerWorker) {
		t.Errorf("received = %d, want %d", got, workers*msgsPerWorker)
	}
}

// ---------------------------------------------------------------------------
// Messages snapshot
// ---------------------------------------------------------------------------

func TestMessages_ReturnsAllPublished(t *testing.T) {
	s := newStream()
	// Subscribe to prevent the messages being ignored.
	_ = s.Subscribe("snap.>", func(_ *stream.Message) error { return nil })

	_ = s.Publish(context.Background(), "snap.a", []byte("1"))
	_ = s.Publish(context.Background(), "snap.b", []byte("2"))
	_ = s.Publish(context.Background(), "snap.c", []byte("3"))

	// Small sleep so publishes complete.
	time.Sleep(10 * time.Millisecond)

	msgs := s.Messages()
	if len(msgs) != 3 {
		t.Errorf("messages count = %d, want 3", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Unsubscribe
// ---------------------------------------------------------------------------

func TestUnsubscribe_StopsDelivery(t *testing.T) {
	s := newStream()
	var count int64

	_ = s.Subscribe("unsub.test", func(_ *stream.Message) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	_ = s.Publish(context.Background(), "unsub.test", []byte("before"))
	time.Sleep(20 * time.Millisecond)

	_ = s.Unsubscribe()

	_ = s.Publish(context.Background(), "unsub.test", []byte("after"))
	time.Sleep(20 * time.Millisecond)

	got := atomic.LoadInt64(&count)
	if got != 1 {
		t.Errorf("count = %d, want 1 (only pre-unsub message)", got)
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestClose_IdempotentSecondClose(t *testing.T) {
	s := newStream()
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// Publishing after close must fail (not panic).
	err := s.Publish(context.Background(), "x", nil)
	if err == nil {
		t.Error("expected error after close")
	}
}
