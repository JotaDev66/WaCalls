package engine

import (
	"errors"
	"sync"

	"wacalls/internal/voip/core"
	"wacalls/internal/voip/media"
)

type SrtpManager struct {
	mu       sync.Mutex
	sendKM   core.SrtpKeyingMaterial
	recvKM   core.SrtpKeyingMaterial
	sendAuth int
	recvAuth int
	send     map[uint32]*media.SrtpContext
	recv     map[uint32]*media.SrtpContext
}

func NewSrtpManager(sendKM, recvKM core.SrtpKeyingMaterial, sendAuth, recvAuth int) *SrtpManager {
	return &SrtpManager{
		sendKM:   sendKM,
		recvKM:   recvKM,
		sendAuth: sendAuth,
		recvAuth: recvAuth,
		send:     map[uint32]*media.SrtpContext{},
		recv:     map[uint32]*media.SrtpContext{},
	}
}

func (m *SrtpManager) Protect(pkt *media.RtpPacket) ([]byte, error) {
	if pkt == nil || pkt.Header == nil {
		return nil, errors.New("srtp: nil packet")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, ok := m.send[pkt.Header.Ssrc]
	if !ok {
		c, err := media.NewSrtpContext(m.sendKM, m.sendAuth)
		if err != nil {
			return nil, err
		}
		m.send[pkt.Header.Ssrc] = c
		ctx = c
	}
	return ctx.Protect(pkt)
}

func (m *SrtpManager) Unprotect(data []byte) (*media.RtpPacket, error) {
	if len(data) < 12 {
		return nil, errors.New("srtp: packet too short")
	}
	ssrc := media.RTPSsrc(data)
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, ok := m.recv[ssrc]
	if !ok {
		c, err := media.NewSrtpContext(m.recvKM, m.recvAuth)
		if err != nil {
			return nil, err
		}
		m.recv[ssrc] = c
		ctx = c
	}
	return ctx.Unprotect(data)
}

func (m *SrtpManager) RekeyRecv(recvKM core.SrtpKeyingMaterial) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recvKM = recvKM
	m.recv = map[uint32]*media.SrtpContext{}
}
