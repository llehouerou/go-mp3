package mp3_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	mp3 "github.com/llehouerou/go-mp3"
	"github.com/llehouerou/go-mp3/internal/testaudio"
)

// corpusCase is one synthesised file plus the facts the decoder must state
// about it. Expectations are derived from the file's own structure, never
// hand-copied from a run, so a wrong number here is a wrong file.
type corpusCase struct {
	name string
	opts testaudio.Options

	// frames is the number of audio frames the decoder should decode,
	// including any Xing header frame (which decodes to silence).
	frames int
	// bytesPerFrame is the decoded PCM bytes per frame: 4608 for MPEG1
	// (1152 samples x 4), 2304 for MPEG2/2.5.
	bytesPerFrame int64
	sampleRate    int
}

func corpus() []corpusCase {
	return []corpusCase{
		{
			name:          "cbr stereo, no xing",
			opts:          testaudio.Options{Frames: 20},
			frames:        20,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "cbr stereo, info header",
			opts:          testaudio.Options{Frames: 20, XingMode: testaudio.Info},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "vbr stereo, xing header",
			opts:          testaudio.Options{Frames: 20, BitratesKbps: []int{128, 192, 96, 320}, XingMode: testaudio.Xing},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "vbr stereo, no xing",
			opts:          testaudio.Options{Frames: 20, BitratesKbps: []int{128, 64, 256}},
			frames:        20,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "mono",
			opts:          testaudio.Options{Frames: 20, Mono: true, XingMode: testaudio.Info},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "48 kHz",
			opts:          testaudio.Options{Frames: 20, SampleRateIndex: 1, XingMode: testaudio.Info},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    48000,
		},
		{
			name:          "mpeg2",
			opts:          testaudio.Options{Version: 2, Frames: 20, XingMode: testaudio.Xing},
			frames:        21,
			bytesPerFrame: 2304,
			sampleRate:    22050,
		},
		{
			name:          "mpeg2 mono",
			opts:          testaudio.Options{Version: 2, Mono: true, Frames: 20, XingMode: testaudio.Xing},
			frames:        21,
			bytesPerFrame: 2304,
			sampleRate:    22050,
		},
		{
			name:          "id3v2 prefix",
			opts:          testaudio.Options{Frames: 20, ID3v2Size: 2048, XingMode: testaudio.Info},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "id3v1 suffix",
			opts:          testaudio.Options{Frames: 20, ID3v1: true, XingMode: testaudio.Info},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "trailing garbage",
			opts:          testaudio.Options{Frames: 20, TrailingGarbage: bytes.Repeat([]byte{0x41}, 4096)},
			frames:        20,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name:          "xing without byte count",
			opts:          testaudio.Options{Frames: 20, XingMode: testaudio.Xing, OmitByteCount: true},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
		{
			name: "lame gapless",
			opts: testaudio.Options{
				Frames: 20, XingMode: testaudio.Xing,
				LAME: true, EncoderDelay: 576, EncoderPadding: 1152,
			},
			frames:        21,
			bytesPerFrame: 4608,
			sampleRate:    44100,
		},
	}
}

// open decodes with gapless disabled, so these assertions describe raw frame
// structure only. Gapless behaviour is covered by gapless_test.go.
func open(t *testing.T, data []byte) *mp3.Decoder {
	t.Helper()
	d, err := mp3.NewDecoderWithOptions(bytes.NewReader(data), mp3.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	return d
}

// TestCorpusLength pins the length the decoder reports for every shape of file.
// This is the assertion the Xing-derived length has to reproduce exactly.
func TestCorpusLength(t *testing.T) {
	for _, tc := range corpus() {
		t.Run(tc.name, func(t *testing.T) {
			d := open(t, testaudio.Build(tc.opts))

			if got := d.SampleRate(); got != tc.sampleRate {
				t.Errorf("SampleRate() = %d, want %d", got, tc.sampleRate)
			}
			if got := d.BytesPerFrame(); got != tc.bytesPerFrame {
				t.Errorf("BytesPerFrame() = %d, want %d", got, tc.bytesPerFrame)
			}
			want := int64(tc.frames) * tc.bytesPerFrame
			if got := d.Length(); got != want {
				t.Errorf("Length() = %d, want %d (%d frames)", got, want, tc.frames)
			}
			if got := d.SampleCount(); got != want/4 {
				t.Errorf("SampleCount() = %d, want %d", got, want/4)
			}
		})
	}
}

// TestCorpusDecodedBytesMatchLength is the strongest invariant available
// without a reference decoder: what Read yields must be exactly what Length
// promised. A length derived from a header instead of a scan must keep it.
func TestCorpusDecodedBytesMatchLength(t *testing.T) {
	for _, tc := range corpus() {
		t.Run(tc.name, func(t *testing.T) {
			d := open(t, testaudio.Build(tc.opts))

			decoded, err := io.ReadAll(d)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if int64(len(decoded)) != d.Length() {
				t.Errorf("decoded %d bytes, Length() = %d", len(decoded), d.Length())
			}
		})
	}
}

// TestCorpusSeekYieldsSameBytes pins seek accuracy: decoding from a seek point
// must produce the same bytes as a straight play-through from the start.
func TestCorpusSeekYieldsSameBytes(t *testing.T) {
	for _, tc := range corpus() {
		t.Run(tc.name, func(t *testing.T) {
			data := testaudio.Build(tc.opts)

			straight, err := io.ReadAll(open(t, data))
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}

			// A frame boundary, a mid-frame offset, and the last frame.
			offsets := []int64{
				tc.bytesPerFrame * 3,
				tc.bytesPerFrame*5 + 1024,
				int64(tc.frames-1) * tc.bytesPerFrame,
			}
			for _, off := range offsets {
				d := open(t, data)
				if _, err := d.Seek(off, io.SeekStart); err != nil {
					t.Fatalf("Seek(%d): %v", off, err)
				}
				got, err := io.ReadAll(d)
				if err != nil {
					t.Fatalf("ReadAll after Seek(%d): %v", off, err)
				}
				if want := straight[off:]; !bytes.Equal(got, want) {
					t.Errorf("Seek(%d): decoded %d bytes, want %d, equal=%v",
						off, len(got), len(want), bytes.Equal(got, want))
				}
			}
		})
	}
}

// TestTruncatedFile covers a download cut short mid-frame. Its Xing header is
// no longer credible, so the length is counted rather than believed.
//
// It also documents a long-standing off-by-one: the scan counts the last frame
// from its header alone, while decoding stops before that frame because its
// body is incomplete, so Length() over-reports by exactly one frame.
func TestTruncatedFile(t *testing.T) {
	full := testaudio.Options{Frames: 20, XingMode: testaudio.Xing}
	frameSize := testaudio.FrameSize(full)

	truncated := full
	truncated.TruncateBytes = frameSize*5 + frameSize/2

	d := open(t, testaudio.Build(truncated))
	decoded, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	const bytesPerFrame = 4608
	if over := d.Length() - int64(len(decoded)); over != bytesPerFrame {
		t.Errorf("Length() = %d, decoded %d bytes: over-reports by %d, want exactly "+
			"one frame (%d)", d.Length(), len(decoded), over, bytesPerFrame)
	}
	if untruncated := int64(21) * bytesPerFrame; d.Length() >= untruncated {
		t.Errorf("Length() = %d, want less than the untruncated %d", d.Length(), untruncated)
	}
}

// TestLyingXingFrameCount is the fixture the floor check has to catch:
// the header claims far more frames than the file holds.
func TestLyingXingFrameCount(t *testing.T) {
	opts := testaudio.Options{Frames: 20, XingMode: testaudio.Xing, XingFrameCount: 20000}

	d := open(t, testaudio.Build(opts))
	decoded, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if int64(len(decoded)) != d.Length() {
		t.Errorf("decoded %d bytes, Length() = %d: a lying frame count must not "+
			"leak into Length()", len(decoded), d.Length())
	}
}

// TestNonSeekableSource covers a stream: a Xing header needs no seeking, so the
// length is known even though seeking is not possible. The two used to be the
// same condition.
func TestNonSeekableSource(t *testing.T) {
	data := testaudio.Build(testaudio.Options{Frames: 20, XingMode: testaudio.Xing})

	d, err := mp3.NewDecoderWithOptions(testaudio.NewUnseekableCounter(data), mp3.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	if want := int64(21) * 4608; d.Length() != want {
		t.Errorf("Length() = %d on a stream with a Xing header, want %d", d.Length(), want)
	}
	if err := d.SeekToSample(0); !errors.Is(err, mp3.ErrNotSeekable) {
		t.Errorf("SeekToSample on a stream: err = %v, want ErrNotSeekable", err)
	}
	if _, err := d.Seek(0, io.SeekStart); !errors.Is(err, mp3.ErrNotSeekable) {
		t.Errorf("Seek on a stream: err = %v, want ErrNotSeekable", err)
	}

	decoded, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if int64(len(decoded)) != d.Length() {
		t.Errorf("decoded %d bytes, Length() = %d", len(decoded), d.Length())
	}
}

// TestNonSeekableWithoutXing pins the other half: no header and no seeking
// means the length genuinely cannot be known.
func TestNonSeekableWithoutXing(t *testing.T) {
	data := testaudio.Build(testaudio.Options{Frames: 20})

	d, err := mp3.NewDecoderWithOptions(testaudio.NewUnseekableCounter(data), mp3.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	if got := d.Length(); got != -1 {
		t.Errorf("Length() = %d on a headerless stream, want -1", got)
	}
	if got := d.Duration(); got != -1 {
		t.Errorf("Duration() = %v on a headerless stream, want -1", got)
	}

	decoded, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if want := 20 * 4608; len(decoded) != want {
		t.Errorf("decoded %d bytes, want %d", len(decoded), want)
	}
}

// TestMPEG25Unsupported pins the decoder's stated boundary: MPEG 2.5 files are
// rejected at open rather than silently mis-decoded.
func TestMPEG25Unsupported(t *testing.T) {
	data := testaudio.Build(testaudio.Options{Version: 25, Mono: true, Frames: 20})

	if _, err := mp3.NewDecoderWithOptions(bytes.NewReader(data), mp3.DecoderOptions{}); err == nil {
		t.Error("NewDecoderWithOptions on an MPEG 2.5 file: want an error")
	}
}
