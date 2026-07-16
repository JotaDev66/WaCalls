package mlow

import "math"

// cpx is a single-precision complex value.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L314-L339
type cpx struct {
	re, im float32
}

func (a cpx) add(b cpx) cpx {
	return cpx{re: a.re + b.re, im: a.im + b.im}
}

func (a cpx) mul(b cpx) cpx {
	return cpx{
		re: a.re*b.re - a.im*b.im,
		im: a.re*b.im + a.im*b.re,
	}
}

// smallestFactor returns the smallest prime factor of n (>= 2).
func smallestFactor(n int) int {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L341-L354
	if n%2 == 0 {
		return 2
	}
	p := 3
	for p*p <= n {
		if n%p == 0 {
			return p
		}
		p += 2
	}
	return n
}

// twTab pairs one visited FFT length with its precomputed twiddle table.
type twTab struct {
	n   int
	tab []cpx
}

// fftTwiddles are the precomputed twiddle factors for one (top-N, sign) FFT. The
// butterfly cos/sin depend only on (n, index), never on the input, so they are
// computed once at scratch init and read in the hot loop. Each table reproduces the
// EXACT inline angle arithmetic (same float32 op order, same math.Cos/Sin), so
// reading from it is bit-identical to the inline recompute; proven by
// TestTwiddleTableIsBitExact and TestFFTMatchesDirectTrigReference.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L356-L439
type fftTwiddles struct {
	combine []twTab // per visited length n, indexed k*p+q (length n*p)
	base    []twTab // per prime base length n, indexed k*n+j (length n*n)
}

// combineTwiddle computes the combine twiddle for (n, k, q, sign) exactly as the
// inline butterfly did. The float32 operand order is load-bearing; do not reorder.
func combineTwiddle(n, k, q int, sign float32) cpx {
	ang := sign * 2.0 * smplPI * float32(k) * float32(q) / float32(n)
	return cpx{re: float32(math.Cos(float64(ang))), im: float32(math.Sin(float64(ang)))}
}

// baseTwiddle computes the prime-base twiddle for (n, k, j, sign) exactly as the
// inline base DFT did (two-step: angK = sign*2*pi*k/n, then ang = angK*j). The
// float32 operand order is load-bearing; do not reorder.
func baseTwiddle(n, k, j int, sign float32) cpx {
	angK := sign * 2.0 * smplPI * float32(k) / float32(n)
	ang := angK * float32(j)
	return cpx{re: float32(math.Cos(float64(ang))), im: float32(math.Sin(float64(ang)))}
}

// newFFTTwiddles builds the twiddle tables for the chain of n values the recursion
// visits from top down, for the given sign.
func newFFTTwiddles(top int, sign float32) fftTwiddles {
	var tw fftTwiddles
	n := top
	for n > 1 {
		p := smallestFactor(n)
		if p == n {
			tab := make([]cpx, 0, n*n)
			for k := range n {
				for j := range n {
					tab = append(tab, baseTwiddle(n, k, j, sign))
				}
			}
			tw.base = append(tw.base, twTab{n: n, tab: tab})
			break
		}
		tab := make([]cpx, 0, n*p)
		for k := range n {
			for q := range p {
				tab = append(tab, combineTwiddle(n, k, q, sign))
			}
		}
		tw.combine = append(tw.combine, twTab{n: n, tab: tab})
		n /= p
	}
	return tw
}

func (t *fftTwiddles) combineFor(n int) []cpx {
	for i := range t.combine {
		if t.combine[i].n == n {
			return t.combine[i].tab
		}
	}
	panic("mlow: combine twiddle table missing")
}

func (t *fftTwiddles) baseFor(n int) []cpx {
	for i := range t.base {
		if t.base[i].n == n {
			return t.base[i].tab
		}
	}
	panic("mlow: base twiddle table missing")
}

// fftScratch is the reusable FFT workspace owned by PercModelState and the encoder's
// LPC path, so the per-frame FFTs run without re-allocating the recursion scratch and
// without recomputing the input-independent butterfly cos/sin. arena backs fftRec's
// per-level sub buffers; cin/spec/tout back the real-FFT pack/unpack; twFwd/twBwd are
// the precomputed twiddles for the two signs. Reuse never touches the arithmetic, so
// the output is bit-identical.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L441-L481
type fftScratch struct {
	arena []cpx
	cin   []cpx
	spec  []cpx
	tout  []cpx
	twFwd fftTwiddles // sign = -1.0
	twBwd fftTwiddles // sign = +1.0
}

func newFFTScratch(n int) *fftScratch {
	return &fftScratch{
		arena: make([]cpx, fftArenaLen(n)),
		cin:   make([]cpx, n),
		spec:  make([]cpx, n),
		tout:  make([]cpx, n),
		twFwd: newFFTTwiddles(n, -1.0),
		twBwd: newFFTTwiddles(n, 1.0),
	}
}

// fftArenaLen is the worst-case fftRec arena length: each level carves n cpx for sub
// then recurses on m = n/p (children are sequential, so they reuse the remainder; the
// bound is additive down one path, not across siblings). 576 -> 1143, 512 -> 1020.
func fftArenaLen(n int) int {
	if n <= 1 {
		return 0
	}
	p := smallestFactor(n)
	if p == n {
		return 0
	}
	return n + fftArenaLen(n/p)
}

// fftRec is the recursive mixed-radix Cooley-Tukey DFT. x holds n inputs at the given
// stride; out is contiguous. scratch is the remaining arena: each non-base level
// splits off the first n entries for its sub buffer and passes the rest down
// (siblings are sequential, so the rest is safely shared). tw supplies the
// precomputed butterfly twiddles for this FFT's sign.
func fftRec(x []cpx, stride, n int, out, scratch []cpx, tw *fftTwiddles) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L483-L532
	if n == 1 {
		out[0] = x[0]
		return
	}
	p := smallestFactor(n)
	if p == n {
		bt := tw.baseFor(n)
		for k := range n {
			var acc cpx
			row := k * n
			for j := range n {
				acc = acc.add(x[j*stride].mul(bt[row+j]))
			}
			out[k] = acc
		}
		return
	}
	m := n / p
	sub, rest := scratch[:n], scratch[n:]
	for q := range p {
		fftRec(x[q*stride:], stride*p, m, sub[q*m:(q+1)*m], rest, tw)
	}
	ct := tw.combineFor(n)
	for k := range n {
		kmod := k % m
		var acc cpx
		row := k * p
		for q := range p {
			acc = acc.add(sub[q*m+kmod].mul(ct[row+q]))
		}
		out[k] = acc
	}
}

// cfft computes the complex FFT of a mixed-radix length into out. The transform sign
// (-1 forward, +1 inverse) is carried by tw; the reference keeps a debug-assert-only
// sign parameter that the Go port drops.
func cfft(input, out, arena []cpx, tw *fftTwiddles) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L534-L540
	fftRec(input, 1, len(input), out, arena, tw)
}

// rfftForwardOrderedSc is the forward real FFT of n real samples, re-packed into the
// ordered REAL layout: f[0]=DC.re, f[1]=Nyquist.re, then [re,im] pairs for bins
// 1..n/2-1. Output length is n. Uses sc.cin/sc.spec/sc.arena as scratch (reused
// across calls).
func rfftForwardOrderedSc(time, f []float32, sc *fftScratch) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L542-L561
	n := len(time)
	cin := sc.cin
	for i := range n {
		cin[i].re = time[i]
		cin[i].im = 0.0
	}
	cfft(cin, sc.spec, sc.arena, &sc.twFwd)
	spec := sc.spec
	f[0] = spec[0].re
	f[1] = spec[n/2].re
	for i := 1; i < n/2; i++ {
		f[2*i] = spec[i].re
		f[2*i+1] = spec[i].im
	}
}
