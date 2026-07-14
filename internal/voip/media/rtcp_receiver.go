package media

import "sync"

// audioClockRate is the RTP timestamp clock for WhatsApp audio (960 ts units / 60 ms frame).
const audioClockRate = 16000

// RTCPReceiverStats tracks reception of one peer RTP stream and produces RFC 3550 report blocks
// (loss, interarrival jitter, extended highest sequence, LSR/DLSR). Thread-safe: the media recv
// path feeds it while the RTCP tx cadence reads a report block.
type RTCPReceiverStats struct {
	mu         sync.Mutex
	clockRate  uint32
	baseSeq    uint32
	maxSeq     uint16
	cycles     uint32
	received   uint32
	initSeq    bool
	expectPrev uint32
	recvPrev   uint32

	jitter    float64
	lastTs    uint32
	lastArriv uint32 // arrival in RTP clock units
	haveTs    bool

	lsr        uint32 // middle 32 bits of the peer's last SR NTP
	lsrArrival uint64 // wall clock (ms) that SR arrived
}

func NewRTCPReceiverStats() *RTCPReceiverStats {
	return &RTCPReceiverStats{clockRate: audioClockRate}
}

// NoteRTP records a received RTP packet (its sequence, timestamp, and arrival wall clock in ms).
func (r *RTCPReceiverStats) NoteRTP(seq uint16, rtpTs uint32, arrivalMs uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.initSeq {
		r.baseSeq = uint32(seq)
		r.maxSeq = seq
		r.initSeq = true
	} else if seq < r.maxSeq && uint16(r.maxSeq-seq) < 0x8000 {
		// reordered/duplicate below max: still counts as received, no seq advance.
	} else {
		if seq < r.maxSeq {
			r.cycles += 0x10000 // wrapped
		}
		r.maxSeq = seq
	}
	r.received++

	// RFC 3550 interarrival jitter: D = (arrival_j - arrival_i) - (ts_j - ts_i), in RTP units.
	arriv := uint32(arrivalMs * uint64(r.clockRate) / 1000)
	if r.haveTs {
		transit := arriv - r.lastArriv
		tsDelta := rtpTs - r.lastTs
		d := int64(transit) - int64(tsDelta)
		if d < 0 {
			d = -d
		}
		r.jitter += (float64(d) - r.jitter) / 16.0
	}
	r.lastTs = rtpTs
	r.lastArriv = arriv
	r.haveTs = true
}

// NoteSenderReport records the peer's last SR for LSR/DLSR. ntpMid is the middle 32 bits of the
// SR's 64-bit NTP timestamp (the low 16 of seconds and high 16 of the fraction).
func (r *RTCPReceiverStats) NoteSenderReport(ntpMid uint32, arrivalMs uint64) {
	r.mu.Lock()
	r.lsr = ntpMid
	r.lsrArrival = arrivalMs
	r.mu.Unlock()
}

// ReportBlock builds the report block about the peer SSRC as of nowMs.
func (r *RTCPReceiverStats) ReportBlock(peerSsrc uint32, nowMs uint64) RTCPReportBlock {
	r.mu.Lock()
	defer r.mu.Unlock()

	extHigh := r.cycles | uint32(r.maxSeq)
	expected := uint32(0)
	if r.initSeq {
		expected = extHigh - r.baseSeq + 1
	}
	cumulativeLost := expected - r.received

	expectedInterval := expected - r.expectPrev
	receivedInterval := r.received - r.recvPrev
	r.expectPrev = expected
	r.recvPrev = r.received
	lostInterval := int64(expectedInterval) - int64(receivedInterval)
	var fraction uint8
	if expectedInterval > 0 && lostInterval > 0 {
		fraction = uint8((lostInterval << 8) / int64(expectedInterval))
	}

	var dlsr uint32
	if r.lsr != 0 && nowMs >= r.lsrArrival {
		dlsr = uint32((nowMs - r.lsrArrival) * 65536 / 1000)
	}

	return RTCPReportBlock{
		SSRC:           peerSsrc,
		FractionLost:   fraction,
		CumulativeLost: cumulativeLost & 0xffffff,
		ExtHighSeq:     extHigh,
		Jitter:         uint32(r.jitter),
		LSR:            r.lsr,
		DLSR:           dlsr,
	}
}
