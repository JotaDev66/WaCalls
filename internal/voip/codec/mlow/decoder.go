package mlow

import "github.com/rs/zerolog"

// MLow top-level decoder: RED strip → TOC routing → active-frame decode (3 chained
// 20 ms internal frames: LSF → pulses → pitch/gains → reconstruct → CELP synthesis)
// → per-packet harmonic postfilter → 60 ms PCM. Cross-frame predictor and synthesis
// history persist across calls (the stream is continuous).
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L1-L218

const opusFrameSamps = 960 // 60 ms @ 16 kHz

// SmplDecoderState is the cross-frame decoder state: LSF predictor, previous NLSF,
// the CELP synthesis state, and the harmonic-postfilter state.
type SmplDecoderState struct {
	Lstate   SmplLsfState
	PrevNLSF []float32
	Celp     *CelpDecState
	Harm     *HarmPostfilterState
}

func newSmplDecoderState() *SmplDecoderState {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L641-L672
	return &SmplDecoderState{Celp: NewCelpDecState(), Harm: NewHarmPostfilterState()}
}

// MlowDecoder is a stateful pure-Go MLow decoder.
type MlowDecoder struct {
	state      *SmplDecoderState
	redundancy int32
	log        zerolog.Logger

	// Diagnostic: body length / bytes consumed by the last active-frame decode, and
	// the RED-detection outcome for the last packet.
	lastBodyLen   int
	lastConsumed  int
	lastWasRed    bool
	lastRedBlocks int
	lastMainToc   byte
}

// LastDecodeStats returns diagnostics for the most recent decode: the active body
// length and bytes consumed, whether the packet was SplitRed, how many redundant
// blocks it carried, and the main frame's TOC byte.
func (d *MlowDecoder) LastDecodeStats() (bodyLen, consumed int, wasRed bool, redBlocks int, mainToc byte) {
	return d.lastBodyLen, d.lastConsumed, d.lastWasRed, d.lastRedBlocks, d.lastMainToc
}

// NewMlowDecoder allocates a fresh decoder.
func NewMlowDecoder(opts ...Option) *MlowDecoder {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L36-L41
	return &MlowDecoder{state: newSmplDecoderState(), log: resolveConfig(opts).log}
}

// SetRedundancy sets the negotiated RED redundancy level (0 = bare frames).
func (d *MlowDecoder) SetRedundancy(n int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L44-L46
	d.redundancy = int32(n)
}

// Reset clears the cross-frame state (call at a stream discontinuity).
func (d *MlowDecoder) Reset() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L49-L51
	d.state = newSmplDecoderState()
}

// Decode decodes one RTP MLow payload into a 60 ms (960-sample) PCM frame, float in [-1, 1].
func (d *MlowDecoder) Decode(payload []byte) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L54-L72
	if len(payload) == 0 {
		d.log.Trace().Msg("decode: empty payload, emitting silence")
		return make([]float32, opusFrameSamps)
	}
	d.lastWasRed = false
	d.lastRedBlocks = 0
	d.lastMainToc = 0
	// SplitRed container (observed in video calls with DTX on). WhatsApp wraps the
	// current frame with redundant copies of previous frames for loss recovery:
	//
	//   0x92 <count> [ <len> <redundant frame> ]*(count-1) <main frame>
	//
	// The main (current) frame is LAST and is a normal bare MLow frame (TOC 0x50 /
	// 0x12 / 0x90). Decode only the main; the redundant copies are FEC. Decoding the
	// raw container as a bare frame range-decodes the count/length/redundancy bytes
	// as audio → noise (the video-call symptom). This is a different layout from
	// red.go's DepackSplitRed (note the extra count byte), so it's parsed here.
	if subs, ok := splitContainer(payload); ok {
		d.lastWasRed = true
		d.lastRedBlocks = len(subs) - 1
		d.lastMainToc = subs[len(subs)-1][0]
		// Each sub-frame is a sequential 60 ms span (NOT redundancy): the container
		// packs the whole RTP interval (e.g. 2×60 ms = the 120 ms ts_delta) as
		// length-delimited bare frames. Decode them all in order and concatenate so
		// the playout matches the timestamp; decoding only the last dropped half the
		// audio → choppy/stuttering.
		var out []float32
		for _, sf := range subs {
			out = append(out, d.decodeFrame(sf)...)
		}
		return out
	}
	return d.decodeFrame(payload)
}

// splitContainer parses a 0x92 multi-frame container into its sequential
// sub-frames: 0x92 <count> [ <len> <frame> ]*(count-1) <last frame = rest>.
// Returns ok=false for anything that isn't a well-formed container so the caller
// decodes it as a bare frame.
func splitContainer(p []byte) ([][]byte, bool) {
	if len(p) < 3 || p[0] != 0x92 {
		return nil, false
	}
	count := int(p[1])
	if count < 2 || count > 8 {
		return nil, false
	}
	frames := make([][]byte, 0, count)
	off := 2
	for i := 0; i < count-1; i++ {
		if off >= len(p) {
			return nil, false
		}
		flen := int(p[off])
		off++
		if off+flen > len(p) {
			return nil, false
		}
		frames = append(frames, p[off:off+flen])
		off += flen
	}
	if off >= len(p) {
		return nil, false
	}
	frames = append(frames, p[off:])
	return frames, true
}

func (d *MlowDecoder) decodeFrame(frame []byte) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L74-L99
	if len(frame) == 0 {
		d.log.Trace().Msg("decode frame: empty, emitting silence")
		return make([]float32, opusFrameSamps)
	}
	toc := ParseSmplTOC(frame[0], d.log)
	// Silence length is driven by frame_ms on a 16 kHz basis so the playback clock
	// stays roughly aligned even for frames we don't decode.
	outLen := 16000 / 1000 * toc.FrameMs
	if outLen <= 0 {
		outLen = opusFrameSamps
	}
	// Operating point: this 1:1 decoder only handles 16 kHz / low_rate=0 / 60 ms
	// ACTIVE frames. Off-point frames MUST NOT reach decodeActiveFrame: it sizes
	// the harmonic postfilter for exactly 3 internal (20 ms) frames, so a 120 ms or
	// 32 kHz TOC makes it emit more samples than the buffer holds and panics
	// ("slice bounds out of range"). Such frames are unvalidated, so → silence.
	//
	// NOTE: we deliberately do NOT gate on toc.SID (bit 7). With DTX/SID enabled,
	// the peer sets bit 7 on EVERY frame as a stream-level flag, including real
	// active audio (e.g. TOC 0x92, vad/bit1 set, 200+ byte payloads). The true
	// silence indicator is !Active (vad==0 && bit1==0): genuine SID/CN frames like
	// 0x90 are inactive and still fall through to silence. Gating on SID (as the
	// reference's golden 60 ms capture did, where active frames had bit7=0) would
	// silence the entire inbound stream once the peer turns DTX on.
	if toc.StdOpus || !toc.Active || toc.SampleRate != 16000 || toc.Flag2 || toc.FrameMs != 60 {
		d.log.Debug().Uint8("toc_byte", frame[0]).Bool("std_opus", toc.StdOpus).Bool("sid", toc.SID).
			Bool("active", toc.Active).Int("frame_ms", toc.FrameMs).Int("sample_rate", toc.SampleRate).
			Bool("low_rate", toc.Flag2).Msg("decode frame: off operating point or inactive, emitting silence")
		return make([]float32, outLen)
	}
	return d.decodeActiveFrame(frame)
}

// internalGroupSize is the base number of 20 ms internal frames (60 ms).
const internalGroupSize = 3

func (d *MlowDecoder) decodeActiveFrame(frame []byte) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L101-L217
	config := int(frame[0]>>2) & 1
	tbl := LoadSmplTables()
	synthT := LoadSmplSynthTables()
	mem := LoadSmplMem()
	dec := NewRangeDecoder(frame[1:])
	lowRate := (frame[0]>>2)&1 != 0
	bodyLen := dec.BodyLen()

	out := make([]float32, 0, 2*internalGroupSize*SmplIntfLen)
	packetLags := make([]float32, 0, 2*internalGroupSize*8)
	var avgNormBr float32

	decodeOne := func(f int) {
		lsf := DecodeSmplLsf(dec, tbl, &d.state.Lstate, config, f)
		pulses := DecodeSmplPulses(dec, mem, SmplIntfLen, 4, 1, int32(config), lsf.Stage1)
		voiced := lsf.Stage1 == 1
		var total int32
		for _, c := range pulses.Subfr {
			total += c
		}
		params := CelpDecParams{Voiced: voiced, SfPulses: pulses.Subfr, TotalPulses: total}
		if voiced {
			pr := DecodeSmplPitch(dec, mem, &d.state.Lstate, SmplIntfLen, 4, int32(config), pulses.Subfr)
			for b := 0; b < 8; b++ {
				v := float64(pr.BlockLags[b])*0.5 + 32.0
				if v > 320.0 {
					v = 320.0
				}
				params.BlockLags[b] = float32(v)
			}
			for sf := 0; sf < 4; sf++ {
				params.AcbgIdx[sf] = pr.GainIdx[sf]
				if pr.FiltIdx[sf] > 0 {
					params.FcbgIdx[sf] = pr.FiltIdx[sf]
				}
			}
		} else {
			g := DecodeSmplGains(dec, mem, 4, pulses.Subfr)
			params.NrgresDbqQ14 = g.GainQ
			params.FcbgIdx = g.NrgRes
		}
		packetLags = append(packetLags, params.BlockLags[:]...)
		avgNormBr += SmplGetNormalizedBitrate(params.TotalPulses, SmplIntfLen)

		nlsf := SmplReconstructNLSF(synthT, int(lsf.Stage1), config, int(lsf.Grid), &lsf.Stage2, d.state.PrevNLSF)
		var sig [SmplIntfLen]float32
		d.state.Celp.SynthFrame(nlsf, int(lsf.Extra), pulses.Pulses, &params, lowRate, SmplIntfLen, sig[:])
		d.state.PrevNLSF = nlsf
		out = append(out, sig[:]...)
	}

	// Decode exactly the main frame: 3 internal 20 ms frames = 60 ms. Trailing
	// bytes left in a larger packet are RED redundancy (copies of PREVIOUS frames,
	// for loss recovery) — NOT more current audio. Decoding them replays old audio
	// (heard as "the peer keeps sending audio while silent") and desyncs the
	// cross-frame predictor, so they are intentionally ignored, exactly as the
	// reference does (fixed 0..3 loop). The DTX silence gaps that make this sound
	// choppy are reconstructed downstream from the RTP timestamps, not here.
	const numInternal = internalGroupSize
	for f := 0; f < numInternal; f++ {
		decodeOne(f)
	}
	d.log.Trace().Int("config", config).Bool("low_rate", lowRate).Int("body_bytes", bodyLen).
		Int("internal_frames", numInternal).Int("consumed", dec.BytesConsumed()).Msg("decode active frame")

	d.lastBodyLen = bodyLen
	d.lastConsumed = dec.BytesConsumed()

	// Per-packet harmonic postfilter (final pitch comb + 48-sample group delay) over the whole packet.
	plen := len(out)
	SmplHarmPostfilter(d.state.Harm, out, plen, packetLags, len(packetLags), avgNormBr/float32(numInternal))

	pcm := make([]float32, len(out))
	for i, v := range out {
		switch {
		case v > 1.0:
			v = 1.0
		case v < -1.0:
			v = -1.0
		}
		pcm[i] = v
	}
	return pcm
}
