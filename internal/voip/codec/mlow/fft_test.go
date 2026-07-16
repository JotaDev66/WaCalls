package mlow

// Test-only allocating wrappers preserving the pre-optimization signatures for the
// FFT unit tests. Production threads a reusable *fftScratch instead. Same
// arithmetic, so output is bit-identical.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/652d25de50822259c387e0442a487b5d8a075cf8/wacore/src/voip/mlow/smpl_perc.rs#L563-L570
// and #L595-L600

func rfftForwardOrdered(time, f []float32) {
	rfftForwardOrderedSc(time, f, newFFTScratch(len(time)))
}

func rfftBackwardOrdered(f, time []float32) {
	rfftBackwardOrderedSc(f, time, newFFTScratch(len(f)))
}
