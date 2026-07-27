//go:build !nativemlow

// Package nativemlow is the optional native SMPL/MLow encoder. Without the
// nativemlow build tag this stub keeps the default build pure Go: WrapEncoder is
// the identity and the encode path stays on internal/voip/codec/mlow.
package nativemlow

import "wacalls/internal/voip/core"

// WrapEncoder returns inner unchanged in the pure-Go build, reporting the "go"
// encode path.
func WrapEncoder(inner core.AudioCodec) (core.AudioCodec, string) { return inner, "go" }

// Available reports whether the native encoder is compiled in.
func Available() bool { return false }
