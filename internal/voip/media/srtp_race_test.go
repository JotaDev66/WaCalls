package media

import (
	"bytes"
	"sync"
	"testing"

	"wacalls/internal/voip/core"
)

func TestSrtpContextConcurrentUnprotect(t *testing.T) {
	callKey := bytes.Repeat([]byte{0x11}, 32)
	sendKM, _ := DerivePerJidSrtpKey(callKey, "self:0@lid")
	recvKM, _ := DerivePerJidSrtpKey(callKey, "peer:0@lid")

	sender, err := NewSrtpSession(sendKM, recvKM, core.SRTPSendAuthTagLen, core.SRTPRecvAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewSrtpSession(recvKM, sendKM, core.SRTPRecvAuthTagLen, core.SRTPSendAuthTagLen)
	if err != nil {
		t.Fatal(err)
	}

	sess := NewWhatsAppOpusSession(0xAABBCCDD)
	payload := bytes.Repeat([]byte{0x42}, 40)
	packets := make([][]byte, 0, 8)
	for i := range 8 {
		pkt := sess.CreatePacketWithDuration(payload, 960, i == 0)
		p, err := sender.Protect(pkt)
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, p)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for i := range 300 {
				_, _ = receiver.Unprotect(packets[i%len(packets)])
			}
		})
	}
	wg.Wait()
}
