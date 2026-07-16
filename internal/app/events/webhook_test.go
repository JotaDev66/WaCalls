package events

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSignWebhookKnownVector(t *testing.T) {
	got := signWebhook("secret", "1700000000", []byte(`{"a":1}`))
	want := "49f24e537407743fa4a0242bb63b94b9a47ee99cbbe071ccd8a22550ae411686"
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
}

func TestWebhookDeliverySignedAndVerified(t *testing.T) {
	type got struct {
		headers http.Header
		body    []byte
	}
	ch := make(chan got, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ch <- got{r.Header.Clone(), b}
	}))
	defer srv.Close()

	d := newWebhookDispatcher(srv.URL, "topsecret", slog.Default())
	go d.run(t.Context())

	d.enqueue("call.ringing", CallRecord{SessionID: "s1", CallID: "c1", Direction: "inbound", Peer: "p", StartedAt: 1, Status: StatusRinging})

	select {
	case g := <-ch:
		var ev webhookEvent
		if err := json.Unmarshal(g.body, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Event != "call.ringing" || ev.Call.CallID != "c1" || ev.ID == "" || ev.SentAt == 0 {
			t.Fatalf("bad envelope: %+v", ev)
		}
		ts := g.headers.Get("X-Wacalls-Timestamp")
		if ts == "" {
			t.Fatal("missing timestamp header")
		}
		if want := "v1=" + signWebhook("topsecret", ts, g.body); g.headers.Get("X-Wacalls-Signature") != want {
			t.Fatalf("signature mismatch: %q", g.headers.Get("X-Wacalls-Signature"))
		}
		if g.headers.Get("X-Wacalls-Event") != "call.ringing" || g.headers.Get("X-Wacalls-Delivery") != ev.ID {
			t.Fatalf("convenience headers wrong: %+v", g.headers)
		}
		if g.headers.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type: %q", g.headers.Get("Content-Type"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery")
	}
}

func TestWebhookRetriesUntilSuccess(t *testing.T) {
	var mu sync.Mutex
	var deliveries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		deliveries = append(deliveries, r.Header.Get("X-Wacalls-Delivery"))
		if len(deliveries) < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := newWebhookDispatcher(srv.URL, "s", slog.Default())
	d.backoff = []time.Duration{time.Millisecond, time.Millisecond}
	go d.run(t.Context())
	d.enqueue("call.ended", CallRecord{CallID: "c1", Status: StatusEnded})

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(deliveries)
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("want 3 attempts, got %d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if deliveries[0] != deliveries[1] || deliveries[1] != deliveries[2] {
		t.Fatalf("delivery id must be stable across retries: %v", deliveries)
	}
}

func TestWebhookQueueFullDropsWithoutBlocking(t *testing.T) {
	d := newWebhookDispatcher("http://127.0.0.1:0", "s", slog.Default())
	d.queue = make(chan webhookEvent, 1)
	done := make(chan struct{})
	go func() {
		d.enqueue("call.ringing", CallRecord{CallID: "c1"})
		d.enqueue("call.ringing", CallRecord{CallID: "c2"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueue must never block")
	}
	if len(d.queue) != 1 {
		t.Fatalf("queue len want 1, got %d", len(d.queue))
	}
}

func TestWebhookNilDispatcherIsSafe(t *testing.T) {
	var d *webhookDispatcher
	d.enqueue("call.ringing", CallRecord{CallID: "c1"})
}

func TestBrokerWebhookTransitions(t *testing.T) {
	events := make(chan webhookEvent, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var ev webhookEvent
		_ = json.Unmarshal(b, &ev)
		events <- ev
	}))
	defer srv.Close()

	b := NewBroker(nil, slog.Default())
	b.webhooks = newWebhookDispatcher(srv.URL, "s", slog.Default())
	go b.webhooks.run(t.Context())

	rec := CallRecord{SessionID: "s1", CallID: "c1", Direction: "outbound", Peer: "p", StartedAt: 1, Status: StatusRinging}
	b.UpsertCall(rec)
	b.UpsertCall(rec)
	rec.Status = StatusConnected
	b.UpsertCall(rec)
	b.EndCall("c1", "user_ended")

	want := []string{"call.ringing", "call.active", "call.ended"}
	for i, w := range want {
		select {
		case ev := <-events:
			if ev.Event != w {
				t.Fatalf("event %d: want %s, got %s", i, w, ev.Event)
			}
			if w == "call.ended" && (ev.Call.EndReason != "user_ended" || ev.Call.Status != StatusEnded) {
				t.Fatalf("ended payload: %+v", ev.Call)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("missing event %s", w)
		}
	}
	select {
	case ev := <-events:
		t.Fatalf("unexpected extra event %s (repeated status must not emit)", ev.Event)
	case <-time.After(150 * time.Millisecond):
	}
}
