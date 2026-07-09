package h264

import (
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

func NewSession(ssrc uint32) *media.RtpSession {
	return media.NewRtpSession(ssrc, core.PayloadTypeWhatsAppH264, 90000, 0)
}
