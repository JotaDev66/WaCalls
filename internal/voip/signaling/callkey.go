package signaling

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

var encryptedCallTags = map[string]bool{"preaccept": true, "accept": true}

func NeedsDecryption(tag string) bool { return encryptedCallTags[tag] }

func GenerateCallID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

func GenerateCallStanzaID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

func EncodeCallKeyMessage(callKey []byte) ([]byte, error) {
	msg := &waE2E.Message{Call: &waE2E.Call{CallKey: callKey}}
	return proto.Marshal(msg)
}

func DecryptCallKeyInNode(ctx context.Context, sock Socket, inner *waBinary.Node, peerJid types.JID) ([]byte, error) {
	encNode := findEncNode(inner)
	if encNode == nil {
		return nil, nil
	}
	return sock.DecryptCallKey(ctx, peerJid, encNode)
}

func DecodeCallKeyPlaintext(plaintext []byte) ([]byte, error) {
	var msg waE2E.Message
	if err := proto.Unmarshal(plaintext, &msg); err != nil {
		return nil, err
	}
	key := msg.GetCall().GetCallKey()
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid callKey: expected 32 bytes, got %d", len(key))
	}
	return key, nil
}
