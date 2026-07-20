package session

import "time"

// staleOfferThreshold: an incoming call offer older than this is treated as a replay of an
// offline-buffered event (the caller has long since given up) rather than a live ring. It sits
// comfortably above real offer-to-receipt latency and below the incoming ring timeout, so a
// genuinely live offer never trips it.
const staleOfferThreshold = 30 * time.Second

// offlineReplayMaxWindow bounds how long the offline-replay window stays open if whatsmeow's
// OfflineSyncCompleted is never observed, so a missed completion cannot suppress live calls
// indefinitely.
const offlineReplayMaxWindow = 30 * time.Second

// isStaleOffer reports whether an inbound call offer should be dropped instead of surfaced as a
// live incoming call. WhatsApp replays offers it buffered while the client was offline; those
// render as ghost ringing calls that only clear on the ring timeout. Two signals catch them: the
// offline-replay window (whatsmeow brackets it with OfflineSyncPreview/OfflineSyncCompleted) and
// the offer's own timestamp (the <call t=...> attribute, surfaced as the event Timestamp).
func isStaleOffer(offerTS time.Time, offlineReplaying bool, now time.Time) bool {
	if offlineReplaying {
		return true
	}
	if !offerTS.IsZero() && now.Sub(offerTS) > staleOfferThreshold {
		return true
	}
	return false
}

// armReplayWindow opens the offline-replay window and schedules its backstop closure. The
// closure is tagged with the generation current at arm-time so a timer armed by an earlier
// OfflineSyncPreview cannot close the window opened by a later one — with reconnects in quick
// succession (the exact scenario where offline replay matters most), the first window's timer
// would otherwise fire mid-way through the second window and reopen the ghost-ring hole.
func (s *Session) armReplayWindow() {
	gen := s.replayGen.Add(1)
	s.offlineReplaying.Store(true)
	window := s.replayWindow
	if window <= 0 {
		window = offlineReplayMaxWindow
	}
	time.AfterFunc(window, func() {
		if s.replayGen.Load() == gen {
			s.offlineReplaying.Store(false)
		}
	})
}
