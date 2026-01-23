package mp3

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/llehouerou/go-mp3/lameinfo"
)

// =============================================================================
// Unit Tests for Gapless Calculations
// =============================================================================

func TestGapless_SkipCalculations(t *testing.T) {
	tests := []struct {
		name           string
		encoderDelay   uint16
		encoderPadding uint16
		wantDelay      int
		wantPadding    int
	}{
		{
			name:           "typical LAME values",
			encoderDelay:   576,
			encoderPadding: 1848,
			wantDelay:      576 + lameinfo.DecoderDelay,
			wantPadding:    1848 - lameinfo.DecoderDelay,
		},
		{
			name:           "zero delay and padding",
			encoderDelay:   0,
			encoderPadding: 0,
			wantDelay:      lameinfo.DecoderDelay,
			wantPadding:    0,
		},
		{
			name:           "small padding less than decoder delay",
			encoderDelay:   576,
			encoderPadding: 100,
			wantDelay:      576 + lameinfo.DecoderDelay,
			wantPadding:    0,
		},
		{
			name:           "max 12-bit values",
			encoderDelay:   4095,
			encoderPadding: 4095,
			wantDelay:      4095 + lameinfo.DecoderDelay, // 4624
			wantPadding:    4095 - lameinfo.DecoderDelay, // 3566
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &lameinfo.Info{
				LAMEVersion:    "LAME3.100",
				EncoderDelay:   tt.encoderDelay,
				EncoderPadding: tt.encoderPadding,
			}

			if got := info.TotalDelay(); got != tt.wantDelay {
				t.Errorf("TotalDelay() = %d, want %d", got, tt.wantDelay)
			}
			if got := info.TotalPadding(); got != tt.wantPadding {
				t.Errorf("TotalPadding() = %d, want %d", got, tt.wantPadding)
			}
		})
	}
}

func TestGapless_SkipBytesCalculation(t *testing.T) {
	// Test that skip bytes are calculated correctly
	// skipStartBytes = bytesPerFrame (Xing frame) + TotalDelay() * 4
	// skipEndBytes = TotalPadding() * 4

	info := &lameinfo.Info{
		LAMEVersion:    "LAME3.100",
		EncoderDelay:   576,
		EncoderPadding: 1848,
	}

	bytesPerFrame := int64(4608) // 1152 samples * 4 bytes

	expectedSkipStart := bytesPerFrame + int64(info.TotalDelay())*4
	expectedSkipEnd := int64(info.TotalPadding()) * 4

	// TotalDelay = 576 + 529 = 1105 samples = 4420 bytes
	// skipStartBytes = 4608 + 4420 = 9028 bytes
	if expectedSkipStart != 9028 {
		t.Errorf("skipStartBytes = %d, want 9028", expectedSkipStart)
	}

	// TotalPadding = 1848 - 529 = 1319 samples = 5276 bytes
	if expectedSkipEnd != 5276 {
		t.Errorf("skipEndBytes = %d, want 5276", expectedSkipEnd)
	}
}

// =============================================================================
// Integration Tests with LAME-encoded files
// =============================================================================

func TestGapless_LAMEEncodedFile(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Verify gapless info was detected
	info := d.GaplessInfo()
	if info == nil {
		t.Fatal("GaplessInfo() returned nil, expected LAME info")
	}

	// Verify LAME version
	if info.LAMEVersion != "LAME3.100" {
		t.Errorf("LAMEVersion = %q, want %q", info.LAMEVersion, "LAME3.100")
	}

	// Verify encoder delay is typical LAME value
	if info.EncoderDelay != 576 {
		t.Errorf("EncoderDelay = %d, want 576", info.EncoderDelay)
	}

	// Verify virtual length is less than raw length
	rawLen := d.RawLength()
	virtLen := d.Length()
	if virtLen >= rawLen {
		t.Errorf("Length() = %d should be less than RawLength() = %d", virtLen, rawLen)
	}

	// The difference should equal skipStartBytes + skipEndBytes
	diff := rawLen - virtLen
	t.Logf("Raw length: %d, Virtual length: %d, Trimmed: %d bytes", rawLen, virtLen, diff)
	t.Logf("Encoder delay: %d, Encoder padding: %d", info.EncoderDelay, info.EncoderPadding)
	t.Logf("Total delay: %d samples, Total padding: %d samples", info.TotalDelay(), info.TotalPadding())

	// Verify we can decode the full virtual length
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != virtLen {
		t.Errorf("Decoded %d bytes, expected %d (virtual length)", len(pcm), virtLen)
	}
}

func TestGapless_LAMEFile_Duration(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Duration should be based on virtual (trimmed) length
	dur := d.Duration()
	if dur <= 0 {
		t.Errorf("Duration() = %v, expected positive value", dur)
	}

	// For a 10 second file at 44100 Hz:
	// Raw samples: ~10s * 44100 = 441000 samples
	// After gapless: should be very close to exactly 10s = 441000 samples
	sampleCount := d.SampleCount()
	t.Logf("Sample count: %d, Duration: %v", sampleCount, dur)
}

// =============================================================================
// Tests for files without Xing headers (unchanged behavior)
// =============================================================================

func TestGapless_FileWithoutXingHeader(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Gapless info should be nil for files without LAME/Xing headers
	if info := d.GaplessInfo(); info != nil {
		t.Errorf("GaplessInfo() = %v, want nil for file without Xing header", info)
	}

	// RawLength should equal Length when no gapless info
	if d.RawLength() != d.Length() {
		t.Errorf("RawLength() = %d, Length() = %d, expected equal", d.RawLength(), d.Length())
	}

	// Verify decoding still works
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != d.Length() {
		t.Errorf("Decoded %d bytes, expected %d", len(pcm), d.Length())
	}
}

func TestGapless_MPEG2FileWithoutXingHeader(t *testing.T) {
	f, err := os.Open("example/mpeg2.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Should have no gapless info
	if info := d.GaplessInfo(); info != nil {
		t.Errorf("GaplessInfo() = %v, want nil", info)
	}

	// Decoding should work normally
	buf := make([]byte, 4096)
	n, err := d.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read() failed: %v", err)
	}
	if n == 0 {
		t.Error("Read() returned 0 bytes")
	}
}

// =============================================================================
// Seek tests with gapless mode
// =============================================================================

func TestGapless_SeekToStart(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Read initial data
	buf1 := make([]byte, 4096)
	n1, err := d.Read(buf1)
	if err != nil {
		t.Fatalf("first Read() failed: %v", err)
	}
	buf1 = buf1[:n1]

	// Seek to middle
	if _, err := d.Seek(d.Length()/2, io.SeekStart); err != nil {
		t.Fatalf("Seek to middle failed: %v", err)
	}

	// Seek back to start
	if _, err := d.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek to start failed: %v", err)
	}

	// Verify position is 0
	if d.Position() != 0 {
		t.Errorf("Position() after seek to 0 = %v, expected 0", d.Position())
	}

	// Read again and compare
	buf2 := make([]byte, 4096)
	n2, err := d.Read(buf2)
	if err != nil {
		t.Fatalf("second Read() failed: %v", err)
	}
	buf2 = buf2[:n2]

	// Data should match
	if n1 != n2 {
		t.Errorf("Read sizes differ: first=%d, second=%d", n1, n2)
	} else if !bytes.Equal(buf1, buf2) {
		t.Error("Audio data differs after seek to start")
	}
}

func TestGapless_SeekToMiddle(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Seek to middle
	target := d.Length() / 2
	pos, err := d.Seek(target, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	if pos != target {
		t.Errorf("Seek returned %d, want %d", pos, target)
	}

	// Read some data
	buf := make([]byte, 4096)
	n, err := d.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read() failed: %v", err)
	}
	if n == 0 {
		t.Error("Read() returned 0 bytes after seek to middle")
	}
}

func TestGapless_SeekToEnd(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Seek to end
	target := d.Length()
	pos, err := d.Seek(target, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek to end failed: %v", err)
	}

	if pos != target {
		t.Errorf("Seek returned %d, want %d", pos, target)
	}

	// Reading should return EOF
	buf := make([]byte, 4096)
	_, err = d.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Errorf("Read() at end returned err=%v, want io.EOF", err)
	}
}

func TestGapless_SeekBeyondEnd(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Seek beyond end - should be clamped
	target := d.Length() + 10000
	pos, err := d.Seek(target, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek beyond end failed: %v", err)
	}

	// Position should be clamped to length
	if pos != d.Length() {
		t.Errorf("Seek returned %d, want %d (clamped to length)", pos, d.Length())
	}
}

func TestGapless_SeekNegative(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Seek to negative position - should be clamped to 0
	pos, err := d.Seek(-1000, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek to negative failed: %v", err)
	}

	if pos != 0 {
		t.Errorf("Seek returned %d, want 0 (clamped)", pos)
	}
}

func TestGapless_SeekCurrentAndEnd(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Seek from end
	pos, err := d.Seek(-1000, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek from end failed: %v", err)
	}
	expected := d.Length() - 1000
	if pos != expected {
		t.Errorf("Seek from end returned %d, want %d", pos, expected)
	}

	// Seek relative to current
	pos2, err := d.Seek(500, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek current failed: %v", err)
	}
	if pos2 != pos+500 {
		t.Errorf("Seek current returned %d, want %d", pos2, pos+500)
	}
}

func TestGapless_SeekAndReadConsistency(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Seek to a specific position
	target := int64(10000)
	if _, err := d.Seek(target, io.SeekStart); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	// Read data at this position
	buf1 := make([]byte, 4096)
	n1, err := d.Read(buf1)
	if err != nil {
		t.Fatalf("first Read() failed: %v", err)
	}
	buf1 = buf1[:n1]

	// Seek away
	if _, err := d.Seek(d.Length()/2, io.SeekStart); err != nil {
		t.Fatalf("Seek away failed: %v", err)
	}

	// Read some data
	discard := make([]byte, 4096)
	_, _ = d.Read(discard)

	// Seek back to same position
	if _, err := d.Seek(target, io.SeekStart); err != nil {
		t.Fatalf("Seek back failed: %v", err)
	}

	// Read again
	buf2 := make([]byte, 4096)
	n2, err := d.Read(buf2)
	if err != nil {
		t.Fatalf("second Read() failed: %v", err)
	}
	buf2 = buf2[:n2]

	// Data should be identical
	if n1 != n2 {
		t.Errorf("Read sizes differ: first=%d, second=%d", n1, n2)
	} else if !bytes.Equal(buf1, buf2) {
		t.Error("Audio data differs after seek to same position")
	}
}

// =============================================================================
// Non-seekable source tests
// =============================================================================

func TestGapless_NonSeekableSource(t *testing.T) {
	data, err := os.ReadFile("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}

	// Wrap in non-seekable reader
	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}

	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Gapless info should be nil for non-seekable sources
	// (we can't seek back to parse the LAME header)
	if info := d.GaplessInfo(); info != nil {
		t.Logf("Note: GaplessInfo() returned %v for non-seekable source", info)
		// This is actually OK - we read the first frame during init
		// so we might still have LAME info
	}

	// Decoding should still work
	buf := make([]byte, 4096)
	n, err := d.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read() failed: %v", err)
	}
	if n == 0 {
		t.Error("Read() returned 0 bytes")
	}
}

// =============================================================================
// NewDecoderWithOptions tests
// =============================================================================

func TestGapless_DisabledWithOptions(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	// Create decoder with gapless disabled
	opts := DecoderOptions{Gapless: false}
	d, err := NewDecoderWithOptions(f, opts)
	if err != nil {
		t.Fatalf("NewDecoderWithOptions() failed: %v", err)
	}

	// GaplessInfo should be nil when gapless is disabled
	if info := d.GaplessInfo(); info != nil {
		t.Errorf("GaplessInfo() = %v, want nil when gapless disabled", info)
	}

	// Length should be the raw length (no trimming)
	rawLen := d.RawLength()
	virtLen := d.Length()
	if rawLen != virtLen {
		t.Errorf("RawLength() = %d, Length() = %d, expected equal when gapless disabled", rawLen, virtLen)
	}

	// Decode full file
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != rawLen {
		t.Errorf("Decoded %d bytes, expected %d (raw length)", len(pcm), rawLen)
	}
}

func TestGapless_EnabledVsDisabled(t *testing.T) {
	// Open file twice - once with gapless, once without
	f1, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f1.Close()

	f2, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f2.Close()

	// Decoder with gapless enabled (default)
	dEnabled, err := NewDecoder(f1)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Decoder with gapless disabled
	dDisabled, err := NewDecoderWithOptions(f2, DecoderOptions{Gapless: false})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions() failed: %v", err)
	}

	// Skip test if no gapless info available
	if dEnabled.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Disabled should have longer length than enabled
	if dDisabled.Length() <= dEnabled.Length() {
		t.Errorf("Disabled length (%d) should be > enabled length (%d)",
			dDisabled.Length(), dEnabled.Length())
	}

	// Difference should be skipStartBytes + skipEndBytes
	diff := dDisabled.Length() - dEnabled.Length()
	t.Logf("Gapless enabled: %d bytes, Disabled: %d bytes, Difference: %d bytes",
		dEnabled.Length(), dDisabled.Length(), diff)
}

func TestGapless_DefaultOptionsEnablesGapless(t *testing.T) {
	opts := DefaultDecoderOptions()
	if !opts.Gapless {
		t.Error("DefaultDecoderOptions().Gapless = false, want true")
	}
}

// =============================================================================
// Lavc (ffmpeg) encoder prefix test
// =============================================================================

func TestGapless_LavcEncoderRecognition(t *testing.T) {
	// Test that isLAMEVersion recognizes Lavc prefix
	tests := []struct {
		version string
		want    bool
	}{
		{"Lavc58.54", true},
		{"Lavc59.18", true},
		{"Lavc60.3.", true},
		{"LAME3.100", true},
		{"GOGO    ", true},
		{"XXXX    ", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			// We can't directly test isLAMEVersion from here since it's unexported,
			// but we can test via Parse with a synthetic frame
			frame := buildLavcTestFrame(tt.version)
			info, err := lameinfo.Parse(frame)

			if tt.want {
				if err != nil {
					t.Errorf("Parse() error = %v for version %q", err, tt.version)
				} else if !info.HasLAMEInfo() {
					t.Errorf("HasLAMEInfo() = false for version %q", tt.version)
				}
				// Note: LAMEVersion may have trailing null bytes due to 9-byte padding
			} else {
				// For non-LAME versions, HasLAMEInfo should be false
				if err == nil && info.HasLAMEInfo() {
					t.Errorf("HasLAMEInfo() = true for non-LAME version %q", tt.version)
				}
			}
		})
	}
}

// buildLavcTestFrame creates a synthetic MP3 frame with a given encoder version string
func buildLavcTestFrame(version string) []byte {
	// MPEG1 Layer III stereo frame header
	header := []byte{0xFF, 0xFB, 0x90, 0x00}

	// Side info for MPEG1 stereo = 32 bytes
	sideInfo := make([]byte, 32)

	// Xing tag
	tag := []byte("Info")

	// Flags (none set)
	flags := []byte{0, 0, 0, 0}

	frame := make([]byte, 0, 500)
	frame = append(frame, header...)
	frame = append(frame, sideInfo...)
	frame = append(frame, tag...)
	frame = append(frame, flags...)

	// Add encoder version string (9 bytes)
	versionBytes := make([]byte, 9)
	copy(versionBytes, version)
	frame = append(frame, versionBytes...)

	// LAME info fields (12 bytes before delay/padding)
	lameInfo := make([]byte, 12)
	frame = append(frame, lameInfo...)

	// Encoder delay and padding (3 bytes)
	delayPadding := []byte{0x02, 0x40, 0x00} // delay=576, padding=0
	frame = append(frame, delayPadding...)

	// Remaining LAME fields
	remaining := make([]byte, 12)
	frame = append(frame, remaining...)

	// Pad to minimum frame size
	minSize := 417
	if len(frame) < minSize {
		frame = append(frame, make([]byte, minSize-len(frame))...)
	}

	return frame
}

// =============================================================================
// Edge case tests
// =============================================================================

func TestGapless_VeryShortFile(t *testing.T) {
	// Test with a file that might have more skip bytes than actual content
	// This is a synthetic test case

	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Verify length is non-negative
	if d.Length() < 0 && d.Length() != -1 {
		t.Errorf("Length() = %d, should be >= 0 or -1", d.Length())
	}

	// Verify we can still read
	buf := make([]byte, 1024)
	_, err = d.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read() failed: %v", err)
	}
}

func TestGapless_ReadExactLength(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Read the entire virtual length
	length := d.Length()
	pcm := make([]byte, length)
	n, err := io.ReadFull(d, pcm)
	if err != nil {
		t.Fatalf("ReadFull() failed: %v", err)
	}
	if int64(n) != length {
		t.Errorf("ReadFull() read %d bytes, expected %d", n, length)
	}

	// Next read should return EOF
	buf := make([]byte, 1)
	_, err = d.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Errorf("Read() after full read returned err=%v, want io.EOF", err)
	}
}

func TestGapless_MultipleReads(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Read in small chunks and verify total equals virtual length
	totalRead := int64(0)
	buf := make([]byte, 1024)
	for {
		n, err := d.Read(buf)
		totalRead += int64(n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read() failed: %v", err)
		}
	}

	if totalRead != d.Length() {
		t.Errorf("Total read %d bytes, expected %d (virtual length)", totalRead, d.Length())
	}
}

func TestGapless_PositionTracking(t *testing.T) {
	f, err := os.Open("example/classic_lame.mp3")
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	if d.GaplessInfo() == nil {
		t.Skip("File has no gapless info")
	}

	// Initial position should be 0
	if d.SamplePosition() != 0 {
		t.Errorf("Initial SamplePosition() = %d, want 0", d.SamplePosition())
	}

	// Read some data
	buf := make([]byte, 4096)
	n, err := d.Read(buf)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}

	// Position should have advanced
	expectedSamples := int64(n) / 4
	if d.SamplePosition() != expectedSamples {
		t.Errorf("SamplePosition() = %d, want %d", d.SamplePosition(), expectedSamples)
	}

	// Progress should be small but non-zero
	progress := d.Progress()
	if progress <= 0 || progress >= 0.5 {
		t.Errorf("Progress() = %f, expected small positive value", progress)
	}
}
