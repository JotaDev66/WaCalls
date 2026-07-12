package call

import (
	"context"
	"time"

	"wacalls/internal/voip/core"
)

type Timeouts struct {
	Ring            time.Duration
	Answer          time.Duration
	MediaConnect    time.Duration
	MediaInactivity time.Duration
	ReconnectGrace  time.Duration
	MaxDuration     time.Duration
}

var DefaultTimeouts = Timeouts{
	Ring:            60 * time.Second,
	Answer:          60 * time.Second,
	MediaConnect:    30 * time.Second,
	MediaInactivity: 5 * time.Second,
	ReconnectGrace:  75 * time.Second,
	MaxDuration:     4 * time.Hour,
}

const defaultWatchdogTick = time.Second

func (m *CallManager) startWatchdog() {
	m.mu.Lock()
	if m.watchdogStop != nil {
		close(m.watchdogStop)
	}
	stop := make(chan struct{})
	m.watchdogStop = stop
	m.mu.Unlock()

	done := m.observer.TrackGoroutine()
	go func() {
		defer done()
		ticker := time.NewTicker(m.watchdogTick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if m.expireIfOverdue() {
					return
				}
			}
		}
	}()
}

func (m *CallManager) expireIfOverdue() bool {
	m.mu.Lock()
	call := m.currentCall
	if call == nil {
		m.mu.Unlock()
		return false
	}
	if call.IsEnded() {
		m.mu.Unlock()
		return true
	}
	deadline, ok := phaseDeadline(call, m.timeouts)
	state := call.StateData.State
	callID := call.CallID
	inactivity := m.timeouts.MediaInactivity
	m.mu.Unlock()

	if state == core.CallStateActive && inactivity > 0 {
		if last := m.lastMediaRecv.Load(); last > 0 && time.Now().UnixMilli()-last > inactivity.Milliseconds() {
			m.forceMediaLost()
			return false
		}
	}

	if !ok || time.Now().Before(deadline) {
		return false
	}
	m.log.Info("call timed out", "call_id", callID, "state", string(state))
	_ = m.EndCall(context.Background(), core.EndCallReasonTimeout)
	return true
}

func phaseDeadline(c *CallInfo, t Timeouts) (time.Time, bool) {
	s := c.StateData
	switch s.State {
	case core.CallStateInitiating, core.CallStateRinging:
		if t.Answer <= 0 {
			return time.Time{}, false
		}
		return c.CreatedAt.Add(t.Answer), true
	case core.CallStateIncomingRinging:
		if t.Ring <= 0 {
			return time.Time{}, false
		}
		return c.CreatedAt.Add(t.Ring), true
	case core.CallStateConnecting:
		if t.MediaConnect <= 0 || s.AcceptedAt == nil {
			return time.Time{}, false
		}
		return s.AcceptedAt.Add(t.MediaConnect), true
	case core.CallStateReconnecting:
		if t.ReconnectGrace <= 0 || s.MediaLostAt == nil {
			return time.Time{}, false
		}
		return s.MediaLostAt.Add(t.ReconnectGrace), true
	case core.CallStateActive, core.CallStateOnHold:
		if t.MaxDuration <= 0 || s.ConnectedAt == nil {
			return time.Time{}, false
		}
		return s.ConnectedAt.Add(t.MaxDuration), true
	default:
		return time.Time{}, false
	}
}
