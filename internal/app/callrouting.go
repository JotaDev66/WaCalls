package app

import (
	"wacalls/internal/voip/signaling"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func callIDFromNode(node *waBinary.Node) string {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return ""
	}
	return info.CallID
}
