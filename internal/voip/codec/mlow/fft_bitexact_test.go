package mlow

import (
	"math"
	"testing"
)

// lcgFloats is the deterministic pseudo-random signal generator shared by the FFT
// tests and benchmarks (same LCG as the reference's fft_roundtrip).
func lcgFloats(seed uint32, n int) []float32 {
	s := seed
	x := make([]float32, n)
	for i := range x {
		s = s*196314165 + 907633515
		x[i] = float32(s>>9)/float32(uint32(1)<<23) - 1.0
	}
	return x
}

func prngCpx(seed uint32, n int) []cpx {
	v := lcgFloats(seed, 2*n)
	x := make([]cpx, n)
	for i := range x {
		x[i] = cpx{re: v[2*i], im: v[2*i+1]}
	}
	return x
}

// fftRecRef is the pre-optimization fftRec (per-call inline trig, per-level
// allocation), preserved as a mechanical copy from fft.go @ develop 551f72a. It is
// the bit-exactness reference for the twiddle-table and scratch-reuse port: any
// bitwise divergence of the optimized path from this function is a fidelity break.
func fftRecRef(x []cpx, stride, n int, sign float32, out []cpx) {
	if n == 1 {
		out[0] = x[0]
		return
	}
	p := smallestFactor(n)
	if p == n {
		for k := range n {
			var acc cpx
			angK := sign * 2.0 * smplPI * float32(k) / float32(n)
			for j := range n {
				ang := angK * float32(j)
				w := cpx{re: float32(math.Cos(float64(ang))), im: float32(math.Sin(float64(ang)))}
				acc = acc.add(x[j*stride].mul(w))
			}
			out[k] = acc
		}
		return
	}
	m := n / p
	sub := make([]cpx, n)
	for q := range p {
		fftRecRef(x[q*stride:], stride*p, m, sign, sub[q*m:(q+1)*m])
	}
	for k := range n {
		kmod := k % m
		var acc cpx
		for q := range p {
			ang := sign * 2.0 * smplPI * float32(k) * float32(q) / float32(n)
			tw := cpx{re: float32(math.Cos(float64(ang))), im: float32(math.Sin(float64(ang)))}
			acc = acc.add(sub[q*m+kmod].mul(tw))
		}
		out[k] = acc
	}
}

func cfftRef(input, out []cpx, sign float32) {
	fftRecRef(input, 1, len(input), sign, out)
}

// rfftForwardOrderedRef / rfftBackwardOrderedRef are mechanical copies of the
// pre-optimization allocating real-FFT wrappers, kept as references so the tests can
// drive the production Sc entry points against them.
func rfftForwardOrderedRef(time, f []float32) {
	n := len(time)
	cin := make([]cpx, n)
	for i := range n {
		cin[i].re = time[i]
	}
	spec := make([]cpx, n)
	cfftRef(cin, spec, -1.0)
	f[0] = spec[0].re
	f[1] = spec[n/2].re
	for i := 1; i < n/2; i++ {
		f[2*i] = spec[i].re
		f[2*i+1] = spec[i].im
	}
}

func rfftBackwardOrderedRef(f, time []float32) {
	n := len(f)
	spec := make([]cpx, n)
	spec[0] = cpx{f[0], 0}
	spec[n/2] = cpx{f[1], 0}
	for i := 1; i < n/2; i++ {
		re := f[2*i]
		im := f[2*i+1]
		spec[i] = cpx{re, im}
		spec[n-i] = cpx{re, -im}
	}
	tout := make([]cpx, n)
	cfftRef(spec, tout, 1.0)
	for i := range n {
		time[i] = tout[i].re
	}
}

// percModelRefState + smplPercModelRef are a mechanical copy of the pre-optimization
// SmplPercModel (fresh allocations every call) so the reused-state production path
// can be compared bit-exactly across consecutive calls, including the frameMs=10
// skip region that only the explicit clear() covers after the port.
type percModelRefState struct {
	buf      [percwNfft]float32
	smthcoef []float32
	windows  percWindows
}

func newPercModelRefState() *percModelRefState {
	fsStep := (percwFsKhz * 1000.0) / float32(percwNfft)
	smthcoef := make([]float32, percwNfft/2+1)
	for i := range percwNfft/2 + 1 {
		percWidthPerBin := percMaskSmth * (fsStep*float32(i) + percMelFcHz) / fsStep
		smthcoef[i] = percWidthPerBin / (percWidthPerBin + 1.0)
	}
	return &percModelRefState{smthcoef: smthcoef, windows: newPercWindows()}
}

func smplPercModelRef(state *percModelRefState, xsubfr []float32, xsubfrLen int, frameMs int32, isLastSubfr int32, lenR int) []float32 {
	srcOff := xsubfrLen - (winNextWbLongLen - winNextWbLen)
	keep := percwNfft - xsubfrLen
	copy(state.buf[0:keep], state.buf[srcOff:srcOff+keep])
	copy(state.buf[keep:keep+xsubfrLen], xsubfr[:xsubfrLen])

	winlen := winPrevPercLen + int(frameMs)*16 + win3LongLen
	skipSamples := percwNfft - winlen

	bufWin := make([]float32, percwNfft)
	smplWindowPerc(&state.windows, state.buf[skipSamples:], bufWin[skipSamples:], winlen, frameMs, isLastSubfr == 0)

	f := make([]float32, percwNfft)
	rfftForwardOrderedRef(bufWin, f)
	f[0] = f[0] * f[0]
	f[1] = f[1] * f[1]
	for i := 1; i < percwNfft/2; i++ {
		f[2*i] = f[2*i]*f[2*i] + f[2*i+1]*f[2*i+1]
		f[2*i+1] = 0.0
	}
	smthFilt(f, state.smthcoef)
	rfftBackwardOrderedRef(f, bufWin)

	r := make([]float32, lenR)
	percScaleVec(bufWin, r, lenR, 1.0/float32(percwNfft))
	return r
}

// TestTwiddleTableIsBitExact is the port of the reference's twiddle_table_is_bit_exact:
// every table entry must equal a fresh call of its generator (guards table population
// and indexing; the generator formulas themselves are guarded against the pre-port
// arithmetic by TestFFTMatchesDirectTrigReference).
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L1028-L1078
func TestTwiddleTableIsBitExact(t *testing.T) {
	for _, top := range []int{percwNfft, 512} {
		for _, sign := range []float32{-1.0, 1.0} {
			tw := newFFTTwiddles(top, sign)
			n := top
			for n > 1 {
				p := smallestFactor(n)
				if p == n {
					bt := tw.baseFor(n)
					for k := range n {
						for j := range n {
							want := baseTwiddle(n, k, j, sign)
							got := bt[k*n+j]
							if math.Float32bits(got.re) != math.Float32bits(want.re) ||
								math.Float32bits(got.im) != math.Float32bits(want.im) {
								t.Fatalf("base mismatch n=%d k=%d j=%d sign=%v", n, k, j, sign)
							}
						}
					}
					break
				}
				ct := tw.combineFor(n)
				for k := range n {
					for q := range p {
						want := combineTwiddle(n, k, q, sign)
						got := ct[k*p+q]
						if math.Float32bits(got.re) != math.Float32bits(want.re) ||
							math.Float32bits(got.im) != math.Float32bits(want.im) {
							t.Fatalf("combine mismatch n=%d k=%d q=%d sign=%v", n, k, q, sign)
						}
					}
				}
				n /= p
			}
		}
	}
}

// TestFFTMatchesDirectTrigReference proves the optimized complex FFT (precomputed
// twiddles + carved arena) is bit-identical to the pre-optimization direct-trig
// recursion, for the production sizes plus a mixed-radix/prime/edge sweep, both signs.
func TestFFTMatchesDirectTrigReference(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 7, 9, 15, 21, 512, 576} {
		for _, sign := range []float32{-1.0, 1.0} {
			x := prngCpx(uint32(12345+n), n)
			want := make([]cpx, n)
			fftRecRef(x, 1, n, sign, want)

			tw := newFFTTwiddles(n, sign)
			got := make([]cpx, n)
			arena := make([]cpx, fftArenaLen(n))
			cfft(x, got, arena, &tw)

			for k := range n {
				if math.Float32bits(got[k].re) != math.Float32bits(want[k].re) ||
					math.Float32bits(got[k].im) != math.Float32bits(want[k].im) {
					t.Fatalf("n=%d sign=%v k=%d: got (%v,%v) want (%v,%v)",
						n, sign, k, got[k].re, got[k].im, want[k].re, want[k].im)
				}
			}
		}
	}
}

// TestRfftMatchesReference drives the production real-FFT entry points (including
// the twFwd/twBwd field selection inside the Sc functions) against the
// pre-optimization references, bit-exactly.
func TestRfftMatchesReference(t *testing.T) {
	for _, n := range []int{512, 576} {
		x := lcgFloats(uint32(4321+n), n)

		got := make([]float32, n)
		rfftForwardOrdered(x, got)
		want := make([]float32, n)
		rfftForwardOrderedRef(x, want)
		for i := range n {
			if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
				t.Fatalf("forward n=%d f[%d]: got %v want %v", n, i, got[i], want[i])
			}
		}

		back := make([]float32, n)
		rfftBackwardOrdered(want, back)
		backRef := make([]float32, n)
		rfftBackwardOrderedRef(want, backRef)
		for i := range n {
			if math.Float32bits(back[i]) != math.Float32bits(backRef[i]) {
				t.Fatalf("backward n=%d time[%d]: got %v want %v", n, i, back[i], backRef[i])
			}
		}
	}
}

// TestPercModelReuseMatchesReference runs several SmplPercModel calls on the SAME
// state (exercising the reused fft scratch and the bufWin clear) against the
// allocating reference, bit-exactly. frameMs=10 forces skipSamples=160 > 0, the one
// path where the reused bufWin is not fully overwritten by the windowing itself.
func TestPercModelReuseMatchesReference(t *testing.T) {
	for _, frameMs := range []int32{10, 20} {
		st := NewPercModelState()
		ref := newPercModelRefState()
		xlen := int(frameMs) * 16
		for call := range 4 {
			x := lcgFloats(uint32(1000*int(frameMs)+call), xlen)
			isLast := int32(call % 2)
			got := SmplPercModel(st, x, xlen, frameMs, isLast, smplMaxLResp)
			want := smplPercModelRef(ref, x, xlen, frameMs, isLast, smplMaxLResp)
			for i := range want {
				if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
					t.Fatalf("frameMs=%d call=%d r[%d]: got %v want %v", frameMs, call, i, got[i], want[i])
				}
			}
		}
	}
}

// TestLpcDctTablesMatchFresh: the sync.Once cached tables equal a fresh build.
func TestLpcDctTablesMatchFresh(t *testing.T) {
	fresh := buildDctTables()
	if *lpcDctTables() != fresh {
		t.Fatal("cached dct tables differ from a fresh build")
	}
}
