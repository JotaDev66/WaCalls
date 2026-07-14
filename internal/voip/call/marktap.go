package call

import (
	"time"

	"wacalls/internal/voip/core"
)

// markTap taps the per-call CallObserver to forward connection-phase marks (STUN/ICE/DTLS/SCTP and
// media first-packet) to the CallManager's OnMark hook, with the elapsed time since the call
// observer was built. The transport marks fire from the relay subsystem and the media mark from the
// call path; composing this into m.observer via MultiObserver reaches all of them through the one
// observer chokepoint without touching Mark. OnMark is invoked on a goroutine because the media
// first-packet mark fires under CallManager.mu, and the broker broadcast must not run under that lock.
type markTap struct {
	core.NopObserver
	cm     *CallManager
	callID string
	start  time.Time
}

func (t markTap) Mark(event string) {
	if t.cm.OnMark == nil {
		return
	}
	elapsed := time.Since(t.start).Milliseconds()
	go t.cm.OnMark(t.callID, event, elapsed)
}
