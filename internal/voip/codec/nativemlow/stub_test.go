//go:build !nativemlow

package nativemlow

import (
	"testing"

	"wacalls/internal/voip/codec/mlow"
)

func TestStubIsIdentity(t *testing.T) {
	if Available() {
		t.Fatal("Available must be false without the nativemlow tag")
	}
	if Mode() != "go" {
		t.Fatalf("Mode=%q, want go", Mode())
	}
	inner, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatal(err)
	}
	if got := WrapEncoder(inner); got != inner {
		t.Fatal("WrapEncoder must return inner unchanged in the pure-Go build")
	}
}
