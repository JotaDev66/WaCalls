package main

import (
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"
)

func wrapCall(from types.JID, inner *waBinary.Node) *waBinary.Node {
	content := []waBinary.Node{}
	if inner != nil {
		content = append(content, *inner)
	}
	return &waBinary.Node{
		Tag:     "call",
		Attrs:   waBinary.Attrs{"from": from},
		Content: content,
	}
}
