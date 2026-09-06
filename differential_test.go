package mp3

import (
	"bytes"
	"os"
	"testing"

	"github.com/llehouerou/go-mp3/internal/testaudio"
)

// The scan is the reference implementation: it learns a file's length by
// counting its frames, which is the only way to be sure. Deriving the length
// from a Xing header is a shortcut, and a shortcut is only worth taking if it
// gives the same answer. These tests compare the two on every file shape, and
// they are the reason the scan must stay reachable rather than be deleted.

// scanLength opens the file and forces a full frame count, bypassing the header.
func scanLength(t *testing.T, data []byte) (raw int64, frames int) {
	t.Helper()

	d, err := NewDecoderWithOptions(bytes.NewReader(data), DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	// Discard whatever the header claimed, then count.
	d.rawLength = invalidLength
	d.scanned = false
	d.frameStarts = nil
	if err := d.scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return d.rawLength, len(d.frameStarts)
}

func TestXingLengthMatchesScan(t *testing.T) {
	cases := []struct {
		name string
		opts testaudio.Options
	}{
		{"cbr, info header", testaudio.Options{Frames: 50, XingMode: Info}},
		{"vbr, xing header", testaudio.Options{Frames: 50, BitratesKbps: []int{128, 192, 96, 320}, XingMode: Xing}},
		{"mono", testaudio.Options{Frames: 50, Mono: true, XingMode: Xing}},
		{"48 kHz", testaudio.Options{Frames: 50, SampleRateIndex: 1, XingMode: Xing}},
		{"mpeg2", testaudio.Options{Version: 2, Frames: 50, XingMode: Xing}},
		{"id3v2 prefix", testaudio.Options{Frames: 50, ID3v2Size: 2048, XingMode: Xing}},
		{"id3v1 suffix", testaudio.Options{Frames: 50, ID3v1: true, XingMode: Xing}},
		{"no byte count", testaudio.Options{Frames: 50, XingMode: Xing, OmitByteCount: true}},
		{"lame gapless", testaudio.Options{Frames: 50, XingMode: Xing, LAME: true, EncoderDelay: 576, EncoderPadding: 1152}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := testaudio.Build(tc.opts)

			d, err := NewDecoderWithOptions(bytes.NewReader(data), DecoderOptions{})
			if err != nil {
				t.Fatalf("NewDecoderWithOptions: %v", err)
			}
			if d.scanned {
				t.Error("opening a file with a Xing header scanned it anyway")
			}

			scanned, frames := scanLength(t, data)
			if d.rawLength != scanned {
				t.Errorf("Xing-derived raw length = %d, scan says %d (%d frames)",
					d.rawLength, scanned, frames)
			}
		})
	}
}

// TestXingLengthMatchesScanOnRealFiles runs the same comparison against real
// encoder output, where the frame-count convention (does it include the header
// frame?) is decided by LAME rather than by this repo's test builder.
func TestXingLengthMatchesScanOnRealFiles(t *testing.T) {
	for _, name := range []string{"example/classic.mp3", "example/classic_lame.mp3", "example/mpeg2.mp3"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Skipf("test file not available: %v", err)
			}

			d, err := NewDecoderWithOptions(bytes.NewReader(data), DecoderOptions{})
			if err != nil {
				t.Fatalf("NewDecoderWithOptions: %v", err)
			}

			scanned, frames := scanLength(t, data)
			if d.rawLength == invalidLength {
				t.Logf("no usable Xing header; length is scanned on demand (%d frames)", frames)
				if got := d.RawLength(); got != scanned {
					t.Errorf("RawLength() = %d, scan says %d", got, scanned)
				}
				return
			}
			if d.rawLength != scanned {
				t.Errorf("Xing-derived raw length = %d, scan says %d (%d frames)",
					d.rawLength, scanned, frames)
			}
		})
	}
}

// TestTruncatedXingFallsBackToScan covers the check that keeps a header from
// promising audio the file does not contain.
func TestTruncatedXingFallsBackToScan(t *testing.T) {
	opts := testaudio.Options{Frames: 200, XingMode: Xing}
	full := testaudio.Build(opts)
	truncated := full[:len(full)/2]

	d, err := NewDecoderWithOptions(bytes.NewReader(truncated), DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	if !d.scanned {
		t.Error("a Xing header claiming twice the file's frames was trusted")
	}

	scanned, _ := scanLength(t, truncated)
	if d.rawLength != scanned {
		t.Errorf("raw length = %d, scan says %d", d.rawLength, scanned)
	}
	if untruncated := int64(201) * 4608; d.rawLength >= untruncated {
		t.Errorf("raw length = %d, want well below the claimed %d", d.rawLength, untruncated)
	}
}

// Aliases so the table above reads in the vocabulary of the file format.
const (
	Xing = testaudio.Xing
	Info = testaudio.Info
)
