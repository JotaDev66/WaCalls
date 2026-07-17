package audio

// jitterBuffer reorders inbound RTP audio packets by sequence number and reports
// losses so the caller can conceal them. Packets arrive off the network in delivery
// order, which can differ from sequence order; releasing them in order also lets the
// stateful codec decode frames in the order they were produced.
//
// It is not safe for concurrent use; the audio extension serializes access under its
// own lock.
type jitterBuffer struct {
	depth   int
	next    uint16
	maxSeen uint16
	started bool
	slots   map[uint16][]byte
}

// jitterResetThreshold is the sequence distance beyond which a jump is treated as a
// stream discontinuity (e.g. DTX resume) and resets the cursor, rather than a loss to
// conceal. ~50 packets at 60 ms is 3 s, far past any real reordering or single loss.
const jitterResetThreshold = 50

type jitterFrame struct {
	payload []byte
	present bool
}

func newJitterBuffer(depth int) *jitterBuffer {
	return &jitterBuffer{depth: depth, slots: map[uint16][]byte{}}
}

// push inserts a packet and returns the frames now ready to play, in sequence order.
// A returned frame is either a present payload or a concealment gap (present=false).
func (j *jitterBuffer) push(seq uint16, payload []byte) []jitterFrame {
	if !j.started {
		j.started = true
		j.next = seq
		j.maxSeen = seq
		j.store(seq, payload)
		return j.drain()
	}

	d := int16(seq - j.next)
	if d > jitterResetThreshold || d < -jitterResetThreshold {
		clear(j.slots)
		j.next = seq
		j.maxSeen = seq
		j.store(seq, payload)
		return j.drain()
	}
	if d < 0 {
		// Older than the playout cursor: its slot was already released. Too late.
		return nil
	}
	j.store(seq, payload)
	if int16(seq-j.maxSeen) > 0 {
		j.maxSeen = seq
	}
	return j.drain()
}

func (j *jitterBuffer) store(seq uint16, payload []byte) {
	j.slots[seq] = append([]byte(nil), payload...)
}

func (j *jitterBuffer) drain() []jitterFrame {
	var out []jitterFrame
	for {
		if p, ok := j.slots[j.next]; ok {
			out = append(out, jitterFrame{payload: p, present: true})
			delete(j.slots, j.next)
			j.next++
			continue
		}
		// The next expected packet is missing. Only conceal it once enough newer
		// packets have piled up behind it, giving a reordered packet time to arrive.
		if int16(j.maxSeen-j.next) >= int16(j.depth) {
			out = append(out, jitterFrame{present: false})
			j.next++
			continue
		}
		break
	}
	return out
}
