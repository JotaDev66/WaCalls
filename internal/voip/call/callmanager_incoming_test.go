package call

import (
	"log/slog"
	"testing"
	"time"

	"wacalls/internal/voip/core"
)

// Incoming media path: once a relay connects for an accepted incoming call
// (state Connecting), onRelayConnected must drive it to Active. This is the
// final link that leaves the UI stuck on "Connecting" when it doesn't fire.
func TestIncomingCallReachesActiveOnRelayConnect(t *testing.T) {
	m := &CallManager{log: slog.Default()}

	call := NewIncomingCall("CID", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	if err := call.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if call.StateData.State != core.CallStateConnecting {
		t.Fatalf("accepted incoming call should be Connecting, got %s", call.StateData.State)
	}
	m.currentCall = call

	// codec is nil, so the silence keepalive started inside is a no-op.
	m.onRelayConnected("", 0)

	if !call.IsActive() {
		t.Fatalf("incoming call should be Active after relay connect, got %s", call.StateData.State)
	}
}

// onRelayConnected must only promote a call that is actually Connecting; a late
// relay-connect on a ringing or ended call must not change its state.
func TestOnRelayConnectedOnlyPromotesConnecting(t *testing.T) {
	m := &CallManager{log: slog.Default()}

	ringing := NewIncomingCall("CID", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	m.currentCall = ringing
	m.onRelayConnected("", 0)
	if ringing.StateData.State != core.CallStateIncomingRinging {
		t.Fatalf("ringing call must stay IncomingRinging, got %s", ringing.StateData.State)
	}

	if m2 := (&CallManager{log: slog.Default()}); m2.currentCall == nil {
		// nil currentCall must not panic.
		m2.onRelayConnected("", 0)
	}
}

func TestOnRelayConnectedFiresOnRelay(t *testing.T) {
	m := &CallManager{log: slog.Default()}
	rtt := 24
	call := NewIncomingCall("CID", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	if err := call.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	call.RelayData = &core.RelayData{Endpoints: []core.RelayEndpoint{
		{IP: "9.9.9.9", Port: 443, RelayName: "sfo1"},
		{IP: "1.2.3.4", Port: 3478, RelayName: "gru1", C2RRtt: &rtt},
	}}
	m.currentCall = call

	type fired struct {
		key    string
		rttMs  int
		hasRtt bool
	}
	got := make(chan fired, 1)
	m.OnRelay = func(callID, relayName string, rttMs int, hasRtt bool) {
		got <- fired{callID + "/" + relayName, rttMs, hasRtt}
	}

	m.onRelayConnected("1.2.3.4", 3478)

	select {
	case v := <-got:
		if v.key != "CID/gru1" {
			t.Fatalf("bad callID/relay: %v", v.key)
		}
		if v.rttMs != 24 || !v.hasRtt {
			t.Fatalf("bad rtt: %d/%v", v.rttMs, v.hasRtt)
		}
	case <-time.After(time.Second):
		t.Fatal("OnRelay not fired")
	}
}

func TestOnRelayConnectedFallsBackToIP(t *testing.T) {
	m := &CallManager{log: slog.Default()}
	call := NewIncomingCall("CID", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	call.RelayData = &core.RelayData{Endpoints: []core.RelayEndpoint{{IP: "9.9.9.9", Port: 443, RelayName: "sfo1"}}}
	m.currentCall = call

	got := make(chan string, 1)
	m.OnRelay = func(_, relayName string, rttMs int, hasRtt bool) {
		if rttMs != 0 || hasRtt {
			t.Errorf("no-match must have zero rtt, got %d/%v", rttMs, hasRtt)
		}
		got <- relayName
	}

	m.onRelayConnected("5.6.7.8", 443)

	select {
	case name := <-got:
		if name != "5.6.7.8" {
			t.Fatalf("fallback name: %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("OnRelay not fired")
	}
}
