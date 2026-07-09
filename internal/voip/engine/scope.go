package engine

import (
	"log/slog"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

type CallScope struct {
	Log             *slog.Logger
	CallID          string
	OwnDeviceJID    string
	PeerDeviceJID   string
	IsVideo         bool
	Relay           core.Relay
	SendAudioFrame  func(encoded []byte, frameSamples int) error
	SendRTP         func(pkt *media.RtpPacket) error
	OnRTP           func(pt uint8, handler func(pkt *media.RtpPacket))
	DeclareSelfSSRC func(ssrc uint32)
}

type Extension interface {
	Name() string
	Attach(scope *CallScope) error
	Detach()
}

func Capability[T any](exts []Extension) (T, bool) {
	for _, e := range exts {
		if t, ok := e.(T); ok {
			return t, true
		}
	}
	var zero T
	return zero, false
}
