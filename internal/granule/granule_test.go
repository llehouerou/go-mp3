package granule_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/llehouerou/go-mp3/internal/frameheader"
	"github.com/llehouerou/go-mp3/internal/granule"
	"github.com/llehouerou/go-mp3/internal/testaudio"
)

type fullReader struct{ *bytes.Reader }

func (r fullReader) ReadFull(p []byte) (int, error) { return io.ReadFull(r.Reader, p) }

// TestReadRealFrames pins the invariants every DSP stage relies on, over real
// LAME frames: lines are zero from Count1 up, Count1 stays inside the granule,
// the band tables are the ones for the frame's sample rate, and short blocks
// are flagged consistently with the block type.
func TestReadRealFrames(t *testing.T) {
	data := testaudio.Build(testaudio.Options{Frames: 8, RealAudio: true})
	src := fullReader{bytes.NewReader(data)}
	var r granule.Reader
	nonZero := false
	for i := range 8 {
		h, _, err := frameheader.Read(src, 0)
		if err != nil {
			t.Fatalf("frame %d: header: %v", i, err)
		}
		if err := r.Read(src, h); err != nil {
			t.Fatalf("frame %d: Read: %v", i, err)
		}
		if len(r.Long) != 23 || len(r.Short) != 14 || r.Long[22] != frameheader.SamplesPerGranule {
			t.Fatalf("frame %d: band tables len %d/%d, want 23/14 ending at 576", i, len(r.Long), len(r.Short))
		}
		for gr := range h.Granules() {
			for ch := range h.NumberOfChannels() {
				c := &r.Ch[gr][ch]
				if c.Count1 < 0 || c.Count1 > frameheader.SamplesPerGranule {
					t.Fatalf("frame %d gr %d ch %d: Count1 = %d", i, gr, ch, c.Count1)
				}
				for k := c.Count1; k < frameheader.SamplesPerGranule; k++ {
					if c.Lines[k] != 0 {
						t.Fatalf("frame %d gr %d ch %d: Lines[%d] = %v above Count1 %d", i, gr, ch, k, c.Lines[k], c.Count1)
					}
				}
				for k := range c.Count1 {
					nonZero = nonZero || c.Lines[k] != 0
				}
				if c.ShortBlocks && c.BlockType != 2 {
					t.Fatalf("frame %d gr %d ch %d: ShortBlocks with BlockType %d", i, gr, ch, c.BlockType)
				}
			}
		}
	}
	if !nonZero {
		t.Fatal("eight real frames decoded to all-zero lines")
	}
}

// frames splits a synthesised file into its frames.
func frames(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for len(data) > 0 {
		h, _, err := frameheader.Read(fullReader{bytes.NewReader(data)}, 0)
		if err != nil {
			t.Fatal(err)
		}
		size, err := h.FrameSize()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, data[:size])
		data = data[size:]
	}
	return out
}

// read feeds one frame to r.
func read(t *testing.T, r *granule.Reader, frame []byte) error {
	t.Helper()
	src := fullReader{bytes.NewReader(frame)}
	h, _, err := frameheader.Read(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return r.Read(src, h)
}

// TestReservoirCarriesAcrossFrames pins the two sides of the bit reservoir: a
// frame whose main data begins in earlier frames decodes differently without
// them, and a Reader joining the stream there still decodes it without error,
// from whatever it has.
func TestReservoirCarriesAcrossFrames(t *testing.T) {
	fs := frames(t, testaudio.Build(testaudio.Options{Frames: 8, RealAudio: true}))

	var continuous granule.Reader
	var want [8][2][2]granule.Channel
	for i, f := range fs {
		if err := read(t, &continuous, f); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		want[i] = continuous.Ch
	}

	differs := false
	for i := 1; i < len(fs); i++ {
		var fresh granule.Reader
		if err := read(t, &fresh, fs[i]); err != nil {
			t.Fatalf("frame %d from a fresh Reader: %v", i, err)
		}
		differs = differs || fresh.Ch != want[i]
	}
	if !differs {
		t.Fatal("no frame depended on the reservoir; the fixture cannot exercise it")
	}
}

// TestRegionCountClamp covers side info whose region counts point past the
// end of the scalefactor band table (region0 + region1 + 2 > 22). mpg123 and
// ffmpeg clamp the second region to the end of the granule rather than reject
// the frame, and so does this decoder.
func TestRegionCountClamp(t *testing.T) {
	frame := testaudio.Build(testaudio.Options{Frames: 1})
	// MPEG1 stereo side info: 20 bits of frame-level fields, then granule 0
	// channel 0. Bit offsets are from the start of the side info.
	w := bitWriter{buf: frame[4:]}
	w.put(20, 12, 100) // part2_3_length
	w.put(32, 9, 10)   // big_values
	w.put(41, 8, 200)  // global_gain
	w.put(69, 4, 15)   // region0_count
	w.put(73, 3, 7)    // region1_count

	var r granule.Reader
	if err := read(t, &r, frame); err != nil {
		t.Fatalf("Read: %v, want the region clamped rather than rejected", err)
	}
	if c := r.Ch[0][0].Count1; c < 0 || c > frameheader.SamplesPerGranule {
		t.Errorf("Count1 = %d", c)
	}
}

type bitWriter struct{ buf []byte }

// put writes the low n bits of v at bit offset off, MSB first.
func (w bitWriter) put(off, n int, v uint) {
	for i := range n {
		bit := (v >> (n - 1 - i)) & 1
		pos := off + i
		w.buf[pos/8] |= byte(bit << (7 - pos%8))
	}
}

// TestBandTablesFollowSampleRate pins which scalefactor band row a frame gets:
// the table is indexed by the header's sampling frequency field (44.1, 48,
// 32 kHz and their MPEG-2 halves), not by layer as its comments once said.
// Rows are ISO/IEC 11172-3 Table B.8 and 13818-3 Table B.2, spot-checked.
func TestBandTablesFollowSampleRate(t *testing.T) {
	tests := []struct {
		name            string
		version, sfreq  int
		long5, short5   int // Long[5], Short[5]
		long21, short12 int // last real band starts
	}{
		{"MPEG1 44.1k", 1, 0, 20, 22, 418, 136},
		{"MPEG1 48k", 1, 1, 20, 22, 384, 126},
		{"MPEG1 32k", 1, 2, 20, 22, 550, 180},
		{"MPEG2 22.05k", 2, 0, 30, 24, 522, 174},
		{"MPEG2 24k", 2, 1, 30, 26, 540, 180},
		{"MPEG2 16k", 2, 2, 30, 26, 522, 174},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := testaudio.Build(testaudio.Options{Version: tt.version, SampleRateIndex: tt.sfreq, Frames: 1})
			var r granule.Reader
			if err := read(t, &r, data); err != nil {
				t.Fatal(err)
			}
			got := [4]int{r.Long[5], r.Short[5], r.Long[21], r.Short[12]}
			want := [4]int{tt.long5, tt.short5, tt.long21, tt.short12}
			if got != want {
				t.Errorf("Long[5] Short[5] Long[21] Short[12] = %v, want %v", got, want)
			}
			if r.Long[22] != 576 || r.Short[13] != 192 {
				t.Errorf("table ends at %d/%d, want 576/192", r.Long[22], r.Short[13])
			}
		})
	}
}

// TestReadTruncated pins the error mode the decoder folds into end-of-audio:
// a frame cut short anywhere in its body is an UnexpectedEOFError, not a
// generic read error and not a panic.
func TestReadTruncated(t *testing.T) {
	data := testaudio.Build(testaudio.Options{Frames: 1, RealAudio: true})
	for _, cut := range []int{4 + 10, len(data) - 1} {
		src := fullReader{bytes.NewReader(data[:cut])}
		h, _, err := frameheader.Read(src, 0)
		if err != nil {
			t.Fatal(err)
		}
		var r granule.Reader
		var truncated *frameheader.UnexpectedEOFError
		if err := r.Read(src, h); !errors.As(err, &truncated) {
			t.Errorf("cut at %d: err = %v, want UnexpectedEOFError", cut, err)
		}
	}
}
