package mp3

import (
	"testing"

	"github.com/llehouerou/go-mp3/lameinfo"
)

// Worked example: a LAME 3.100 file at 44.1 kHz stereo (4608 bytes per frame)
// with the encoder's usual delay of 576 and a padding of 1848. The decoder
// delay of 529 is added to the head and subtracted from the tail, so the head
// trim is one Xing frame + 1105 samples and the tail trim is 1319 samples.
const (
	bpf       = 4608
	lameStart = 9028 // 4608 + 1105*4
	lameEnd   = 5276 // 1319*4
)

func TestTrimFor(t *testing.T) {
	tests := []struct {
		name       string
		info       *lameinfo.Info
		start, end int64
	}{
		{"no header", nil, 0, 0},
		{"Xing without LAME tag", &lameinfo.Info{}, bpf + 529*4, 0},
		{"LAME 576/1848", &lameinfo.Info{LAMEVersion: "LAME3.100", EncoderDelay: 576, EncoderPadding: 1848}, lameStart, lameEnd},
		{"padding below decoder delay", &lameinfo.Info{LAMEVersion: "LAME3.100", EncoderDelay: 576, EncoderPadding: 100}, lameStart, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimFor(tt.info, bpf)
			if got.start != tt.start || got.end != tt.end {
				t.Errorf("trimFor() = {%d %d}, want {%d %d}", got.start, got.end, tt.start, tt.end)
			}
		})
	}
}

func TestTrimVirtualLength(t *testing.T) {
	tests := []struct {
		name string
		tr   trim
		raw  int64
		want int64
	}{
		{"zero trim is identity", trim{}, 100 * bpf, 100 * bpf},
		{"LAME 100 frames", trim{lameStart, lameEnd}, 100 * bpf, 100*bpf - lameStart - lameEnd},
		{"trim larger than file clamps to zero", trim{lameStart, lameEnd}, bpf, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tr.virtualLength(tt.raw); got != tt.want {
				t.Errorf("virtualLength(%d) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestTrimRawOf(t *testing.T) {
	tests := []struct {
		name    string
		tr      trim
		virtual int64
		want    int64
	}{
		{"zero trim is identity", trim{}, 1000, 1000},
		{"LAME start of audio", trim{lameStart, lameEnd}, 0, lameStart},
		{"LAME mid-file", trim{lameStart, lameEnd}, 1000, lameStart + 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tr.rawOf(tt.virtual); got != tt.want {
				t.Errorf("rawOf(%d) = %d, want %d", tt.virtual, got, tt.want)
			}
		})
	}
}
