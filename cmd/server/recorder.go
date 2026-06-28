package main

import (
	"encoding/binary"
	"os"
	"sync"
)

type Recorder struct {
	mu      sync.Mutex
	f       *os.File
	samples uint32
}

func NewRecorder(path string) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r := &Recorder{f: f}
	if err := r.writeHeaderLocked(0); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// writeHeaderLocked writes (or overwrites) the 44-byte WAV header.
// Must be called with mu held or before the recorder is shared.
func (r *Recorder) writeHeaderLocked(dataBytes uint32) error {
	const (
		sampleRate = 16000
		channels   = 1
		bitDepth   = 16
		byteRate   = sampleRate * channels * bitDepth / 8
		blockAlign = channels * bitDepth / 8
	)
	if _, err := r.f.Seek(0, 0); err != nil {
		return err
	}
	var buf [44]byte
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 36+dataBytes)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:], channels)
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], byteRate)
	binary.LittleEndian.PutUint16(buf[32:], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:], bitDepth)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], dataBytes)
	_, err := r.f.Write(buf[:])
	return err
}

// Write appends PCM samples to the recording. Safe to call from multiple goroutines.
func (r *Recorder) Write(pcm []float32) {
	if len(pcm) == 0 {
		return
	}
	buf := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(s*32767)))
	}
	r.mu.Lock()
	_, _ = r.f.Write(buf)
	r.samples += uint32(len(pcm))
	r.mu.Unlock()
}

// Close finalises the WAV header with the correct data size and closes the file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dataBytes := r.samples * 2 // 16-bit mono: 2 bytes per sample
	if err := r.writeHeaderLocked(dataBytes); err != nil {
		r.f.Close()
		return err
	}
	return r.f.Close()
}
