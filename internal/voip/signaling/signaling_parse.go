package signaling

import (
	"wacalls/internal/voip/core"
	"wacalls/internal/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
)

type NodeInfo struct {
	Tag            string
	PeerJid        string
	CallID         string
	PeerPlatform   string
	PeerAppVersion string
	EpochID        string
	Timestamp      string
	InnerNode      *waBinary.Node
}

func ExtractNodeInfo(node *waBinary.Node) *NodeInfo {
	children := wanode.NodeChildren(node)
	if len(children) == 0 {
		return nil
	}
	inner := children[0]
	return &NodeInfo{
		Tag:            inner.Tag,
		PeerJid:        wanode.AttrString(node.Attrs, "from"),
		CallID:         wanode.AttrString(inner.Attrs, "call-id"),
		PeerPlatform:   wanode.AttrString(node.Attrs, "platform"),
		PeerAppVersion: wanode.AttrString(node.Attrs, "version"),
		EpochID:        wanode.AttrString(inner.Attrs, "e"),
		Timestamp:      wanode.AttrString(inner.Attrs, "t"),
		InnerNode:      &inner,
	}
}

func ExtractRelayEndpoints(node *waBinary.Node) []core.RelayEndpoint {
	var relays []core.RelayEndpoint

	parseRelay := func(n *waBinary.Node) {
		ip := wanode.AttrString(n.Attrs, "ip")
		token := wanode.AttrString(n.Attrs, "token")
		if ip == "" || token == "" {
			return
		}
		key := wanode.AttrString(n.Attrs, "relay-key")
		if key == "" {
			key = wanode.AttrString(n.Attrs, "key")
		}
		ep := core.RelayEndpoint{
			IP:      ip,
			Port:    wanode.AttrInt(n.Attrs, "port", core.WARelayPort),
			Token:   token,
			Key:     key,
			RelayID: wanode.AttrInt(n.Attrs, "relay-id", 0),
		}
		if wanode.HasAttr(n.Attrs, "c2r-rtt") {
			v := wanode.AttrInt(n.Attrs, "c2r-rtt", 0)
			ep.C2RRtt = &v
		}
		ep.IsFNA = wanode.AttrString(n.Attrs, "is_fna") == "1"
		relays = append(relays, ep)
	}

	for _, child := range wanode.NodeChildren(node) {
		switch child.Tag {
		case "relay":
			parseRelay(&child)
		case "relays":
			for _, rn := range wanode.NodeChildren(&child) {
				if rn.Tag == "relay" {
					parseRelay(&rn)
				}
			}
		}
	}

	sortRelaysByRtt(relays)
	return relays
}

func findEncNode(inner *waBinary.Node) *waBinary.Node {
	for _, c := range wanode.NodeChildren(inner) {
		if c.Tag == "enc" && wanode.HasAttr(c.Attrs, "type") {
			return &c
		}
	}
	for _, c := range wanode.NodeChildren(inner) {
		if c.Tag != "destination" {
			continue
		}
		for _, toNode := range wanode.NodeChildren(&c) {
			if toNode.Tag != "to" {
				continue
			}
			for _, e := range wanode.NodeChildren(&toNode) {
				if e.Tag == "enc" && wanode.HasAttr(e.Attrs, "type") {
					return &e
				}
			}
		}
	}
	return nil
}
