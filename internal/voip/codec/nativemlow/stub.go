//go:build !nativemlow

// Package nativemlow is the optional native SMPL/MLow encoder. Without the
// nativemlow build tag this stub keeps the default build pure Go: WrapEncoder is
// the identity and the encode path stays on internal/voip/codec/mlow.
package nativemlow

import "wacalls/internal/voip/core"

// WrapEncoder returns inner unchanged in the pure-Go build.
func WrapEncoder(inner core.AudioCodec) core.AudioCodec { return inner }

// Available reports whether the native encoder is compiled in.
func Available() bool { return false }

// Mode names the active encode path for operator logs.
func Mode() string { return "go" }
