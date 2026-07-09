package core

type CallObserver interface {
	Mark(event string)
	AddMem(bytes int64)
	ReleaseMem(bytes int64)
	TrackGoroutine() (done func())
	End(result, reason string)
}

type NopObserver struct{}

func (NopObserver) Mark(string)            {}
func (NopObserver) AddMem(int64)           {}
func (NopObserver) ReleaseMem(int64)       {}
func (NopObserver) TrackGoroutine() func() { return func() {} }
func (NopObserver) End(string, string)     {}

var _ CallObserver = NopObserver{}
