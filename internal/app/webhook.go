package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const (
	webhookQueueSize   = 256
	webhookTimeout     = 10 * time.Second
	webhookMaxAttempts = 3
)

type webhookEvent struct {
	ID     string     `json:"id"`
	Event  string     `json:"event"`
	SentAt int64      `json:"sentAt"`
	Call   CallRecord `json:"call"`
}

type webhookDispatcher struct {
	url     string
	secret  string
	queue   chan webhookEvent
	client  *http.Client
	log     *slog.Logger
	backoff []time.Duration
}

func newWebhookDispatcher(url, secret string, log *slog.Logger) *webhookDispatcher {
	return &webhookDispatcher{
		url:     url,
		secret:  secret,
		queue:   make(chan webhookEvent, webhookQueueSize),
		client:  &http.Client{Timeout: webhookTimeout},
		log:     log,
		backoff: []time.Duration{time.Second, 5 * time.Second},
	}
}

func (d *webhookDispatcher) enqueue(event string, rec CallRecord) {
	if d == nil {
		return
	}
	ev := webhookEvent{ID: newDeliveryID(), Event: event, SentAt: time.Now().UnixMilli(), Call: rec}
	select {
	case d.queue <- ev:
	default:
		d.log.Warn("webhook queue full, dropping event", "event", event, "call_id", rec.CallID)
	}
}

func newDeliveryID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (d *webhookDispatcher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-d.queue:
			d.deliver(ctx, ev)
		}
	}
}

func (d *webhookDispatcher) deliver(ctx context.Context, ev webhookEvent) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	for attempt := 0; attempt < webhookMaxAttempts; attempt++ {
		if attempt > 0 && !sleepCtx(ctx, d.backoff[attempt-1]) {
			return
		}
		if d.post(ctx, ev, body) {
			return
		}
	}
	d.log.Error("webhook delivery failed", "event", ev.Event, "call_id", ev.Call.CallID, "attempts", webhookMaxAttempts)
}

func (d *webhookDispatcher) post(ctx context.Context, ev webhookEvent, body []byte) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return false
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wacalls-Event", ev.Event)
	req.Header.Set("X-Wacalls-Delivery", ev.ID)
	req.Header.Set("X-Wacalls-Timestamp", ts)
	req.Header.Set("X-Wacalls-Signature", "v1="+signWebhook(d.secret, ts, body))
	resp, err := d.client.Do(req)
	if err != nil {
		d.log.Warn("webhook post failed", "event", ev.Event, "err", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func signWebhook(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
