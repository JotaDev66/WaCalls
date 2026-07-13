package media

import "testing"

func TestReceiverInOrderNoLoss(t *testing.T) {
	r := NewRTCPReceiverStats()
	for i := range 10 {
		r.NoteRTP(uint16(100+i), uint32(1000+i*960), uint64(i*60))
	}
	rb := r.ReportBlock(0xabc, 1000)
	if rb.ExtHighSeq != 109 {
		t.Errorf("extHigh = %d, want 109", rb.ExtHighSeq)
	}
	if rb.CumulativeLost != 0 {
		t.Errorf("cumulative lost = %d, want 0", rb.CumulativeLost)
	}
	if rb.FractionLost != 0 {
		t.Errorf("fraction lost = %d, want 0", rb.FractionLost)
	}
	if rb.Jitter != 0 {
		t.Errorf("jitter = %d, want 0 for perfect pacing", rb.Jitter)
	}
}

func TestReceiverLoss(t *testing.T) {
	r := NewRTCPReceiverStats()
	for i := range 10 {
		if i == 3 || i == 4 { // drop seq 103, 104
			continue
		}
		r.NoteRTP(uint16(100+i), uint32(1000+i*960), uint64(i*60))
	}
	rb := r.ReportBlock(0xabc, 1000)
	if rb.ExtHighSeq != 109 {
		t.Fatalf("extHigh = %d, want 109", rb.ExtHighSeq)
	}
	if rb.CumulativeLost != 2 { // expected 10, received 8
		t.Errorf("cumulative lost = %d, want 2", rb.CumulativeLost)
	}
	if rb.FractionLost == 0 {
		t.Error("fraction lost = 0, want > 0")
	}
}

func TestReceiverJitter(t *testing.T) {
	r := NewRTCPReceiverStats()
	arr := uint64(0)
	for i := range 20 {
		if i%2 == 0 {
			arr += 40
		} else {
			arr += 80
		}
		r.NoteRTP(uint16(1+i), uint32(1000+i*960), arr)
	}
	if rb := r.ReportBlock(0xabc, 2000); rb.Jitter == 0 {
		t.Error("jitter = 0, want > 0 for jittery arrivals")
	}
}

func TestReceiverLSRDLSR(t *testing.T) {
	r := NewRTCPReceiverStats()
	r.NoteSenderReport(0xd5b873e2, 1000)
	rb := r.ReportBlock(0xabc, 1500) // 500 ms later
	if rb.LSR != 0xd5b873e2 {
		t.Errorf("LSR = %#x, want 0xd5b873e2", rb.LSR)
	}
	if rb.DLSR != 32768 { // 500 ms in units of 1/65536 s
		t.Errorf("DLSR = %d, want 32768", rb.DLSR)
	}
}

func TestReceiverSeqWrap(t *testing.T) {
	r := NewRTCPReceiverStats()
	r.NoteRTP(65534, 1000, 0)
	r.NoteRTP(65535, 1960, 60)
	r.NoteRTP(0, 2920, 120)
	r.NoteRTP(1, 3880, 180)
	if rb := r.ReportBlock(0xabc, 200); rb.ExtHighSeq != 0x10001 {
		t.Errorf("extHigh = %#x, want 0x10001", rb.ExtHighSeq)
	}
}
