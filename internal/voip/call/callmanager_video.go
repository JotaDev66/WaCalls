package call

import (
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/engine"
)

func (m *CallManager) FeedCapturedVideo(au []byte) {
	if v, ok := engine.Capability[core.VideoSink](m.extensions); ok {
		v.FeedAU(au)
	}
}
