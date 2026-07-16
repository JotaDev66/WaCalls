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
	inner, err := mlow.NewMLowCodec(mlow.DefaultCodecOptions)
	if err != nil {
		t.Fatal(err)
	}
	got, mode := WrapEncoder(inner)
	if got != inner {
		t.Fatal("WrapEncoder must return inner unchanged in the pure-Go build")
	}
	if mode != "go" {
		t.Fatalf("mode=%q, want go", mode)
	}
}
