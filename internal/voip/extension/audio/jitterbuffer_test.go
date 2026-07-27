package audio

import "testing"

func seqs(frames []jitterFrame) string {
	out := make([]byte, 0, len(frames))
	for _, f := range frames {
		if f.present {
			out = append(out, f.payload[0])
		} else {
			out = append(out, '.')
		}
	}
	return string(out)
}

func pay(b byte) []byte { return []byte{b} }

func TestJitterInOrderReleasesImmediately(t *testing.T) {
	j := newJitterBuffer(3)
	got := ""
	got += seqs(j.push(10, pay('a')))
	got += seqs(j.push(11, pay('b')))
	got += seqs(j.push(12, pay('c')))
	if got != "abc" {
		t.Fatalf("in-order stream must release with no added latency, got %q", got)
	}
}

func TestJitterReordersLatePacket(t *testing.T) {
	j := newJitterBuffer(3)
	var got string
	got += seqs(j.push(10, pay('a'))) // releases a, next=11
	got += seqs(j.push(12, pay('c'))) // 11 missing, hold
	got += seqs(j.push(11, pay('b'))) // 11 arrives -> release b then c
	if got != "abc" {
		t.Fatalf("a late packet must be reordered into place, got %q", got)
	}
}

func TestJitterConcealsLossAfterDepth(t *testing.T) {
	j := newJitterBuffer(3)
	var got string
	got += seqs(j.push(10, pay('a'))) // a, next=11
	got += seqs(j.push(12, pay('c'))) // hold (11 missing)
	got += seqs(j.push(13, pay('d'))) // hold
	got += seqs(j.push(14, pay('e'))) // maxSeen-next = 14-11 = 3 >= depth -> conceal 11, flush c d e
	if got != "a.cde" {
		t.Fatalf("a lost packet must be concealed after depth newer packets, got %q", got)
	}
}

func TestJitterDropsTooLatePacket(t *testing.T) {
	j := newJitterBuffer(3)
	var got string
	got += seqs(j.push(10, pay('a')))
	got += seqs(j.push(11, pay('b')))
	got += seqs(j.push(10, pay('x'))) // 10 already released; too late
	if got != "ab" {
		t.Fatalf("a packet older than the playout cursor must be dropped, got %q", got)
	}
}

func TestJitterResetsOnLargeJump(t *testing.T) {
	j := newJitterBuffer(3)
	var got string
	got += seqs(j.push(10, pay('a')))    // a, next=11
	got += seqs(j.push(12, pay('c')))    // held
	got += seqs(j.push(10000, pay('z'))) // huge forward jump (DTX resume) -> reset, release z
	if got != "az" {
		t.Fatalf("a large discontinuity must reset instead of concealing the gap, got %q", got)
	}
}

func TestJitterHandlesSequenceWraparound(t *testing.T) {
	j := newJitterBuffer(3)
	var got string
	got += seqs(j.push(65534, pay('a')))
	got += seqs(j.push(65535, pay('b')))
	got += seqs(j.push(0, pay('c'))) // wraps past 65535
	got += seqs(j.push(1, pay('d')))
	if got != "abcd" {
		t.Fatalf("uint16 sequence wraparound must be contiguous, got %q", got)
	}
}
