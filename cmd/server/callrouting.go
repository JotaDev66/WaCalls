package main

import (
	"wacalls/internal/voip/signaling"

	waBinary "github.com/polymorfa/hypermeow/binary"
)

func callIDFromNode(node *waBinary.Node) string {
	info := signaling.ExtractNodeInfo(node)
	if info == nil {
		return ""
	}
	return info.CallID
}
