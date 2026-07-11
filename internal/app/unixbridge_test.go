package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"wacalls/internal/voip/call"
	"wacalls/internal/voip/core"
)

type fakeLocalStart struct {
	callID string
	to     string
	call   *fakeLocalCall
}

type fakeLocalBackend struct {
	starts chan fakeLocalStart
	err    error
}

func newFakeLocalBackend() *fakeLocalBackend {
	return &fakeLocalBackend{starts: make(chan fakeLocalStart, 8)}
}

func (f *fakeLocalBackend) StartOutbound(_ context.Context, callID, to string) (localPCMCall, error) {
	if f.err != nil {
		return nil, f.err
	}
	localCall := newFakeLocalCall()
	f.starts <- fakeLocalStart{callID: callID, to: to, call: localCall}
	return localCall, nil
}

type fakeLocalCall struct {
	inbound  chan []byte
	events   chan localCallEvent
	sent     chan []byte
	fallback chan []byte
	flushes  chan struct{}
	ends     chan string
}

func newFakeLocalCall() *fakeLocalCall {
	return &fakeLocalCall{
		inbound: make(chan []byte, 4), events: make(chan localCallEvent, 4),
		sent: make(chan []byte, 4), fallback: make(chan []byte, 4),
		flushes: make(chan struct{}, 4), ends: make(chan string, 4),
	}
}

func (f *fakeLocalCall) InboundPCM() <-chan []byte     { return f.inbound }
func (f *fakeLocalCall) Events() <-chan localCallEvent { return f.events }
func (f *fakeLocalCall) SendPCM(payload []byte) error {
	f.sent <- append([]byte(nil), payload...)
	return nil
}
func (f *fakeLocalCall) PlayFallback(payload []byte) error {
	f.fallback <- append([]byte(nil), payload...)
	return nil
}
func (f *fakeLocalCall) FlushPlayback() error {
	f.flushes <- struct{}{}
	return nil
}
func (f *fakeLocalCall) End(_ context.Context, reason string) error {
	f.ends <- reason
	return nil
}

func TestUnixPCMServerEndToEndProtocolAndPermissions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := newFakeLocalBackend()
	dir := filepath.Join(shortTempDir(t), "ligacao-ai")
	socketPath := filepath.Join(dir, "wacalls.sock")
	server := newUnixPCMServer(socketPath, backend, slog.Default())
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	assertPerm(t, dir, 0o750)
	assertPerm(t, socketPath, 0o660)
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("expected Unix socket, info=%v err=%v", info, err)
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	writeControlForTest(t, conn, map[string]any{
		"op": "start", "call_id": "call-123", "to": "+5511999999999",
		"sample_rate": 16000, "channels": 1,
	})
	started := awaitValue(t, backend.starts)
	if started.callID != "call-123" || started.to != "+5511999999999" {
		t.Fatalf("unexpected start: %+v", started)
	}

	started.call.events <- localCallEvent{Event: "ringing"}
	event := readEventForTest(t, conn)
	if event.Event != "ringing" || event.CallID != "call-123" {
		t.Fatalf("unexpected event: %+v", event)
	}
	started.call.inbound <- []byte{1, 0, 2, 0}
	kind, payload, err := readUnixFrame(conn)
	if err != nil {
		t.Fatalf("read inbound PCM: %v", err)
	}
	if kind != unixFrameInboundPCM || string(payload) != string([]byte{1, 0, 2, 0}) {
		t.Fatalf("unexpected inbound PCM kind=%x payload=%v", kind, payload)
	}

	if err := writeUnixFrame(conn, unixFrameOutboundPCM, []byte{3, 0, 4, 0}); err != nil {
		t.Fatalf("write outbound PCM: %v", err)
	}
	if got := awaitValue(t, started.call.sent); string(got) != string([]byte{3, 0, 4, 0}) {
		t.Fatalf("outbound PCM=%v", got)
	}
	writeControlForTest(t, conn, map[string]any{"op": "flush_playback"})
	awaitValue(t, started.call.flushes)
	if err := writeUnixFrame(conn, unixFrameFallbackPCM, []byte{5, 0, 6, 0}); err != nil {
		t.Fatalf("write fallback PCM: %v", err)
	}
	if got := awaitValue(t, started.call.fallback); string(got) != string([]byte{5, 0, 6, 0}) {
		t.Fatalf("fallback PCM=%v", got)
	}
	writeControlForTest(t, conn, map[string]any{"op": "end", "reason": "test_complete"})
	if got := awaitValue(t, started.call.ends); got != "test_complete" {
		t.Fatalf("end reason=%q", got)
	}
}

func TestUnixPCMServerRejectsSecondActiveSessionAndReleasesCapacity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := newFakeLocalBackend()
	socketPath := filepath.Join(shortTempDir(t), "run", "wacalls.sock")
	server := newUnixPCMServer(socketPath, backend, slog.Default())
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	first := dialUnixForTest(t, socketPath)
	writeStartForTest(t, first, "call-1")
	firstStart := awaitValue(t, backend.starts)

	second := dialUnixForTest(t, socketPath)
	writeStartForTest(t, second, "call-2")
	busy := readEventForTest(t, second)
	if busy.Event != "busy" || busy.Reason != "capacity" || busy.CallID != "call-2" {
		t.Fatalf("unexpected busy event: %+v", busy)
	}
	_ = second.Close()
	select {
	case unexpected := <-backend.starts:
		t.Fatalf("second active call reached backend: %+v", unexpected)
	default:
	}

	writeControlForTest(t, first, map[string]any{"op": "end", "reason": "finished"})
	awaitValue(t, firstStart.call.ends)
	_ = first.SetReadDeadline(time.Now().Add(time.Second))
	_, _, _ = readUnixFrame(first)
	_ = first.Close()

	third := dialUnixForTest(t, socketPath)
	defer third.Close()
	writeStartForTest(t, third, "call-3")
	thirdStart := awaitValue(t, backend.starts)
	if thirdStart.callID != "call-3" {
		t.Fatalf("capacity was not released: %+v", thirdStart)
	}
}

func TestUnixPCMServerRejectsInvalidStartWithoutCallingBackend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := newFakeLocalBackend()
	socketPath := filepath.Join(shortTempDir(t), "run", "wacalls.sock")
	server := newUnixPCMServer(socketPath, backend, slog.Default())
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	conn := dialUnixForTest(t, socketPath)
	defer conn.Close()
	writeControlForTest(t, conn, map[string]any{
		"op": "start", "call_id": "bad", "to": "5511999999999",
		"sample_rate": 44100, "channels": 2,
	})
	event := readEventForTest(t, conn)
	if event.Event != "error" || event.Error == "" {
		t.Fatalf("unexpected error event: %+v", event)
	}
	select {
	case unexpected := <-backend.starts:
		t.Fatalf("invalid start reached backend: %+v", unexpected)
	default:
	}
}

func TestValidateStartCommandRejectsUnsafeCallID(t *testing.T) {
	for _, callID := range []string{"", "call id", "call\nlog", "chamada/1"} {
		if err := validateStartCommand(callID, "+5511999999999", 16000, 1); err == nil {
			t.Fatalf("unsafe call ID %q was accepted", callID)
		}
	}
	if err := validateStartCommand("call_123.v1:attempt-2", "+5511999999999", 16000, 1); err != nil {
		t.Fatalf("safe call ID rejected: %v", err)
	}
}

func TestUnixPCMServerClosesAfterRemoteTerminalEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := newFakeLocalBackend()
	socketPath := filepath.Join(shortTempDir(t), "run", "wacalls.sock")
	server := newUnixPCMServer(socketPath, backend, slog.Default())
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	conn := dialUnixForTest(t, socketPath)
	defer conn.Close()
	writeStartForTest(t, conn, "call-busy")
	started := awaitValue(t, backend.starts)
	started.call.events <- localCallEvent{Event: "busy", Reason: "busy"}
	event := readEventForTest(t, conn)
	if event.Event != "busy" || event.CallID != "call-busy" {
		t.Fatalf("unexpected terminal event: %+v", event)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := readUnixFrame(conn)
	if err == nil {
		t.Fatal("terminal event did not close Unix connection")
	}
	if reason := awaitValue(t, started.call.ends); reason != "worker_disconnected" {
		t.Fatalf("terminal cleanup reason=%q", reason)
	}
}

func TestUnixPCMServerRefusesToReplaceRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "wacalls.sock")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newUnixPCMServer(path, newFakeLocalBackend(), slog.Default())
	if err := server.Start(context.Background()); err == nil {
		t.Fatal("expected regular socket path to be rejected")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "do not replace" {
		t.Fatalf("regular file was changed: data=%q err=%v", data, err)
	}
}

func TestUnixPCMServerRefusesInsecureExistingDirectoryWithoutChangingIt(t *testing.T) {
	dir := filepath.Join(shortTempDir(t), "insecure")
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	server := newUnixPCMServer(filepath.Join(dir, "wacalls.sock"), newFakeLocalBackend(), slog.Default())
	if err := server.Start(context.Background()); err == nil {
		t.Fatal("expected insecure socket directory to be rejected")
	}
	assertPerm(t, dir, 0o777)
}

type fakeLocalMedia struct {
	mu           sync.Mutex
	feeds        chan []float32
	flushes      int
	drainStarted chan struct{}
	drainRelease chan struct{}
	ends         chan core.EndCallReason
}

func newFakeLocalMedia() *fakeLocalMedia {
	return &fakeLocalMedia{
		feeds: make(chan []float32, 8), drainStarted: make(chan struct{}, 1),
		drainRelease: make(chan struct{}), ends: make(chan core.EndCallReason, 1),
	}
}

func (f *fakeLocalMedia) FeedCapturedPCM(pcm []float32) {
	f.feeds <- append([]float32(nil), pcm...)
}
func (f *fakeLocalMedia) FlushCapturedPCM() {
	f.mu.Lock()
	f.flushes++
	f.mu.Unlock()
}
func (f *fakeLocalMedia) WaitCapturedPCMDrained(ctx context.Context) error {
	f.drainStarted <- struct{}{}
	select {
	case <-f.drainRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fakeLocalMedia) EndCall(_ context.Context, reason core.EndCallReason) error {
	f.ends <- reason
	return nil
}

func TestManagedLocalCallPlaysFullFallbackBeforeEnding(t *testing.T) {
	mediaCall := newFakeLocalMedia()
	managed := newManagedLocalCall("call-fallback")
	managed.bound(nil, "internal", mediaCall)
	defer managed.closed()

	if err := managed.PlayFallback([]byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("PlayFallback: %v", err)
	}
	managed.mu.Lock()
	playback := managed.playback
	managed.mu.Unlock()
	// The fallback is padded to one 60 ms codec frame and paced as three 20 ms
	// chunks. Enqueue sends the first chunk immediately.
	first := awaitValue(t, mediaCall.feeds)
	playback.pumpOnce()
	second := awaitValue(t, mediaCall.feeds)
	playback.pumpOnce()
	third := awaitValue(t, mediaCall.feeds)
	if len(first)+len(second)+len(third) != unixCodecFrameBytes/2 {
		t.Fatalf("fallback samples=%d, want %d", len(first)+len(second)+len(third), unixCodecFrameBytes/2)
	}

	endResult := make(chan error, 1)
	go func() { endResult <- managed.End(context.Background(), "agent_error") }()
	awaitValue(t, mediaCall.drainStarted)
	select {
	case reason := <-mediaCall.ends:
		t.Fatalf("call ended before PCM drained: %s", reason)
	default:
	}
	close(mediaCall.drainRelease)
	if reason := awaitValue(t, mediaCall.ends); reason != core.EndCallReasonFailed {
		t.Fatalf("mapped end reason=%q", reason)
	}
	if err := awaitValue(t, endResult); err != nil {
		t.Fatalf("End: %v", err)
	}
	mediaCall.mu.Lock()
	flushes := mediaCall.flushes
	mediaCall.mu.Unlock()
	if flushes == 0 {
		t.Fatal("fallback did not clear stale playback first")
	}
}

func TestManagedLocalCallFlushDropsQueuedAgentAudio(t *testing.T) {
	mediaCall := newFakeLocalMedia()
	managed := newManagedLocalCall("call-flush")
	managed.bound(nil, "internal", mediaCall)
	defer managed.closed()

	if err := managed.SendPCM(make([]byte, unixPCMChunkBytes*2)); err != nil {
		t.Fatalf("SendPCM: %v", err)
	}
	if err := managed.FlushPlayback(); err != nil {
		t.Fatalf("FlushPlayback: %v", err)
	}
	managed.mu.Lock()
	playback := managed.playback
	managed.mu.Unlock()
	playback.pumpOnce()
	select {
	case frame := <-mediaCall.feeds:
		t.Fatalf("flushed PCM was still played: %d samples", len(frame))
	default:
	}
	mediaCall.mu.Lock()
	flushes := mediaCall.flushes
	mediaCall.mu.Unlock()
	if flushes != 1 {
		t.Fatalf("media flushes=%d, want 1", flushes)
	}
}

func TestManagedLocalCallMapsAndDeduplicatesStateEvents(t *testing.T) {
	managed := newManagedLocalCall("external-call")
	managed.onState(&call.CallInfo{StateData: call.CallStateData{State: core.CallStateRinging}})
	managed.onState(&call.CallInfo{StateData: call.CallStateData{State: core.CallStateRinging}})
	managed.onState(&call.CallInfo{StateData: call.CallStateData{State: core.CallStateEnded, EndReason: core.EndCallReasonBusy}})

	first := awaitValue(t, managed.events)
	second := awaitValue(t, managed.events)
	if first.Event != "ringing" || second.Event != "busy" || second.CallID != "external-call" {
		t.Fatalf("unexpected events: first=%+v second=%+v", first, second)
	}
	select {
	case extra := <-managed.events:
		t.Fatalf("duplicate event emitted: %+v", extra)
	default:
	}
}

func TestReadUnixFrameRejectsOversizedPayloadBeforeAllocation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		header := []byte{unixFrameOutboundPCM, 0, 0x10, 0, 1}
		_, err := client.Write(header)
		done <- err
	}()
	_, _, err := readUnixFrame(server)
	if err == nil {
		t.Fatal("oversized frame was accepted")
	}
	if writeErr := awaitValue(t, done); writeErr != nil {
		t.Fatalf("write header: %v", writeErr)
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions %s=%#o, want %#o", path, got, want)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wc-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func dialUnixForTest(t *testing.T, path string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

func writeStartForTest(t *testing.T, conn net.Conn, callID string) {
	t.Helper()
	writeControlForTest(t, conn, map[string]any{
		"op": "start", "call_id": callID, "to": "+5511999999999",
		"sample_rate": 16000, "channels": 1,
	})
}

func writeControlForTest(t *testing.T, conn net.Conn, command map[string]any) {
	t.Helper()
	payload, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeUnixFrame(conn, unixFrameControlRequest, payload); err != nil {
		t.Fatalf("write control: %v", err)
	}
}

func readEventForTest(t *testing.T, conn net.Conn) localCallEvent {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	kind, payload, err := readUnixFrame(conn)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if kind != unixFrameControlEvent {
		t.Fatalf("frame kind=%x, want event", kind)
	}
	var event localCallEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return event
}

func awaitValue[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for test signal")
		var zero T
		return zero
	}
}

func TestWriteAllHandlesShortWrites(t *testing.T) {
	writer := &shortWriter{maximum: 2}
	if err := writeAll(writer, []byte("abcdef")); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	if string(writer.data) != "abcdef" || writer.writes < 3 {
		t.Fatalf("short writes not handled: data=%q writes=%d", writer.data, writer.writes)
	}
}

type shortWriter struct {
	maximum int
	data    []byte
	writes  int
}

func (w *shortWriter) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, errors.New("empty write")
	}
	n := w.maximum
	if len(payload) < n {
		n = len(payload)
	}
	w.data = append(w.data, payload[:n]...)
	w.writes++
	return n, nil
}

var _ io.Writer = (*shortWriter)(nil)
