#!/usr/bin/env bash
# Build the native MLow codec (edgardmessias/opus_mlow) into native/libopus_mlow.a,
# which the `-tags mlow` cgo build links via `-lopus_mlow`.
#
# Requires: git, cmake, a C toolchain (ninja is used if available).
#   macOS:  brew install cmake ninja
#   Debian: sudo apt install build-essential cmake ninja-build
set -euo pipefail

repo="https://github.com/edgardmessias/opus_mlow.git"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="${TMPDIR:-/tmp}/wacalls-opus_mlow"

gen=()
if command -v ninja >/dev/null 2>&1; then
  gen=(-G Ninja)
fi

rm -rf "$work"
git clone --depth 1 "$repo" "$work"
cmake -S "$work" -B "$work/build" "${gen[@]}" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
cmake --build "$work/build"

lib="$(find "$work/build" -name 'libopus.a' -print -quit || true)"
if [[ -z "$lib" ]]; then
  echo "error: libopus.a not found under $work/build" >&2
  exit 1
fi

mkdir -p "$root/native"
cp "$lib" "$root/native/libopus_mlow.a"
echo "Wrote $root/native/libopus_mlow.a"
echo "Run: CGO_ENABLED=1 go run -tags mlow ./cmd/server"
