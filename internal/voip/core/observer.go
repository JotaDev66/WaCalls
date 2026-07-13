package core

type CallObserver interface {
	Mark(event string)
	SrtpRecvDrop(reason string)
	AddMem(bytes int64)
	ReleaseMem(bytes int64)
	TrackGoroutine() (done func())
	End(result, reason string)
}

type NopObserver struct{}

func (NopObserver) Mark(string)            {}
func (NopObserver) SrtpRecvDrop(string)    {}
func (NopObserver) AddMem(int64)           {}
func (NopObserver) ReleaseMem(int64)       {}
func (NopObserver) TrackGoroutine() func() { return func() {} }
func (NopObserver) End(string, string)     {}

var _ CallObserver = NopObserver{}

const (
	MarkTransportSTUN     = "transport.stun"
	MarkTransportICE      = "transport.ice"
	MarkTransportDTLS     = "transport.dtls"
	MarkTransportSCTPOpen = "transport.sctp_open"
	MarkMediaFirstPacket  = "media.first_packet"
)

func MultiObserver(obs ...CallObserver) CallObserver {
	switch len(obs) {
	case 0:
		return NopObserver{}
	case 1:
		return obs[0]
	}
	return multiObserver(obs)
}

type multiObserver []CallObserver

func (m multiObserver) Mark(event string) {
	for _, o := range m {
		o.Mark(event)
	}
}

func (m multiObserver) SrtpRecvDrop(reason string) {
	for _, o := range m {
		o.SrtpRecvDrop(reason)
	}
}

func (m multiObserver) AddMem(bytes int64) {
	for _, o := range m {
		o.AddMem(bytes)
	}
}

func (m multiObserver) ReleaseMem(bytes int64) {
	for _, o := range m {
		o.ReleaseMem(bytes)
	}
}

func (m multiObserver) TrackGoroutine() func() {
	dones := make([]func(), len(m))
	for i, o := range m {
		dones[i] = o.TrackGoroutine()
	}
	return func() {
		for _, d := range dones {
			d()
		}
	}
}

func (m multiObserver) End(result, reason string) {
	for _, o := range m {
		o.End(result, reason)
	}
}
