package lameinfo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// buildTestFrame creates a synthetic MP3 frame with a Xing/LAME header for testing.
func buildTestFrame(opts testFrameOptions) []byte {
	// Build MPEG1 Layer III stereo frame header
	// Sync: 0xFFE (11 bits)
	// Version: 11 (MPEG1)
	// Layer: 01 (Layer III)
	// Protection: 1 (no CRC)
	// Bitrate: 1001 (128kbps for MPEG1 Layer III)
	// Sampling: 00 (44100 Hz)
	// Padding: 0
	// Private: 0
	// Channel: 00 (stereo)
	// Mode ext: 00
	// Copyright: 0
	// Original: 0
	// Emphasis: 00
	header := []byte{0xFF, 0xFB, 0x90, 0x00}

	// Side info for MPEG1 stereo = 32 bytes (filled with zeros)
	sideInfo := make([]byte, 32)

	// Build Xing/Info tag
	var tag []byte
	if opts.isXing {
		tag = []byte("Xing")
	} else {
		tag = []byte("Info")
	}

	// Flags
	flags := make([]byte, 4)
	flags[3] = byte(opts.flags)

	frame := make([]byte, 0, 500)
	frame = append(frame, header...)
	frame = append(frame, sideInfo...)
	frame = append(frame, tag...)
	frame = append(frame, flags...)

	// Add optional fields based on flags
	if opts.flags&FlagFrameCount != 0 {
		fc := make([]byte, 4)
		fc[0] = byte(opts.frameCount >> 24)
		fc[1] = byte(opts.frameCount >> 16)
		fc[2] = byte(opts.frameCount >> 8)
		fc[3] = byte(opts.frameCount)
		frame = append(frame, fc...)
	}

	if opts.flags&FlagByteCount != 0 {
		bc := make([]byte, 4)
		bc[0] = byte(opts.byteCount >> 24)
		bc[1] = byte(opts.byteCount >> 16)
		bc[2] = byte(opts.byteCount >> 8)
		bc[3] = byte(opts.byteCount)
		frame = append(frame, bc...)
	}

	if opts.flags&FlagTOC != 0 {
		toc := make([]byte, 100)
		for i := range toc {
			toc[i] = byte(i) // Simple linear TOC for testing
		}
		frame = append(frame, toc...)
	}

	if opts.flags&FlagVBRScale != 0 {
		vs := make([]byte, 4)
		vs[0] = byte(opts.vbrScale >> 24)
		vs[1] = byte(opts.vbrScale >> 16)
		vs[2] = byte(opts.vbrScale >> 8)
		vs[3] = byte(opts.vbrScale)
		frame = append(frame, vs...)
	}

	// Add LAME tag if requested
	if opts.lameVersion != "" {
		// LAME version string (9 bytes, padded with spaces if shorter)
		version := make([]byte, 9)
		copy(version, opts.lameVersion)
		frame = append(frame, version...)

		// LAME info fields (12 bytes before delay/padding)
		lameInfo := make([]byte, 12)
		frame = append(frame, lameInfo...)

		// Encoder delay and padding (3 bytes, 12 bits each)
		delayPadding := make([]byte, 3)
		delayPadding[0] = byte(opts.encoderDelay >> 4)
		delayPadding[1] = byte(opts.encoderDelay<<4) | byte(opts.encoderPadding>>8)
		delayPadding[2] = byte(opts.encoderPadding)
		frame = append(frame, delayPadding...)

		// Remaining LAME fields (to complete the tag)
		remaining := make([]byte, 12)
		frame = append(frame, remaining...)
	}

	// Pad to minimum frame size
	minSize := 417 // Typical MPEG1 Layer III 128kbps frame size
	if len(frame) < minSize {
		frame = append(frame, make([]byte, minSize-len(frame))...)
	}

	return frame
}

type testFrameOptions struct {
	isXing         bool
	flags          uint32
	frameCount     uint32
	byteCount      uint32
	vbrScale       uint32
	lameVersion    string
	encoderDelay   uint16
	encoderPadding uint16
}

func TestParse_XingHeader(t *testing.T) {
	frame := buildTestFrame(testFrameOptions{
		isXing:     true,
		flags:      FlagFrameCount | FlagByteCount,
		frameCount: 1000,
		byteCount:  500000,
	})

	info, err := Parse(frame)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !info.IsXing {
		t.Error("IsXing = false, want true")
	}
	if !info.HasFrameCount() {
		t.Error("HasFrameCount() = false, want true")
	}
	if info.FrameCount != 1000 {
		t.Errorf("FrameCount = %d, want 1000", info.FrameCount)
	}
	if !info.HasByteCount() {
		t.Error("HasByteCount() = false, want true")
	}
	if info.ByteCount != 500000 {
		t.Errorf("ByteCount = %d, want 500000", info.ByteCount)
	}
	if info.HasTOC() {
		t.Error("HasTOC() = true, want false")
	}
	if info.HasVBRScale() {
		t.Error("HasVBRScale() = true, want false")
	}
	if info.HasLAMEInfo() {
		t.Error("HasLAMEInfo() = true, want false")
	}
}

func TestParse_InfoHeader(t *testing.T) {
	frame := buildTestFrame(testFrameOptions{
		isXing:     false, // Info tag (CBR)
		flags:      FlagFrameCount,
		frameCount: 2000,
	})

	info, err := Parse(frame)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if info.IsXing {
		t.Error("IsXing = true, want false")
	}
	if info.FrameCount != 2000 {
		t.Errorf("FrameCount = %d, want 2000", info.FrameCount)
	}
}

func TestParse_AllFlags(t *testing.T) {
	frame := buildTestFrame(testFrameOptions{
		isXing:     true,
		flags:      FlagFrameCount | FlagByteCount | FlagTOC | FlagVBRScale,
		frameCount: 5000,
		byteCount:  2500000,
		vbrScale:   75,
	})

	info, err := Parse(frame)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !info.HasFrameCount() || info.FrameCount != 5000 {
		t.Errorf("FrameCount = %d, want 5000", info.FrameCount)
	}
	if !info.HasByteCount() || info.ByteCount != 2500000 {
		t.Errorf("ByteCount = %d, want 2500000", info.ByteCount)
	}
	if !info.HasTOC() {
		t.Error("HasTOC() = false, want true")
	}
	// Check TOC values
	for i := range 100 {
		if info.TOC[i] != byte(i) {
			t.Errorf("TOC[%d] = %d, want %d", i, info.TOC[i], i)
			break
		}
	}
	if !info.HasVBRScale() || info.VBRScale != 75 {
		t.Errorf("VBRScale = %d, want 75", info.VBRScale)
	}
}

func TestParse_LAMEInfo(t *testing.T) {
	frame := buildTestFrame(testFrameOptions{
		isXing:         true,
		flags:          FlagFrameCount,
		frameCount:     3000,
		lameVersion:    "LAME3.100",
		encoderDelay:   576,
		encoderPadding: 1848,
	})

	info, err := Parse(frame)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !info.HasLAMEInfo() {
		t.Fatal("HasLAMEInfo() = false, want true")
	}
	if info.LAMEVersion != "LAME3.100" {
		t.Errorf("LAMEVersion = %q, want %q", info.LAMEVersion, "LAME3.100")
	}
	if info.EncoderDelay != 576 {
		t.Errorf("EncoderDelay = %d, want 576", info.EncoderDelay)
	}
	if info.EncoderPadding != 1848 {
		t.Errorf("EncoderPadding = %d, want 1848", info.EncoderPadding)
	}
}

func TestParse_TotalDelay(t *testing.T) {
	// Without LAME info
	info := &Info{}
	if got := info.TotalDelay(); got != DecoderDelay {
		t.Errorf("TotalDelay() without LAME = %d, want %d", got, DecoderDelay)
	}

	// With LAME info
	info = &Info{
		LAMEVersion:  "LAME3.100",
		EncoderDelay: 576,
	}
	expected := 576 + DecoderDelay
	if got := info.TotalDelay(); got != expected {
		t.Errorf("TotalDelay() with LAME = %d, want %d", got, expected)
	}
}

func TestParse_TotalPadding(t *testing.T) {
	// Without LAME info
	info := &Info{}
	if got := info.TotalPadding(); got != 0 {
		t.Errorf("TotalPadding() without LAME = %d, want 0", got)
	}

	// With LAME info, padding > decoder delay
	info = &Info{
		LAMEVersion:    "LAME3.100",
		EncoderPadding: 1848,
	}
	expected := 1848 - DecoderDelay
	if got := info.TotalPadding(); got != expected {
		t.Errorf("TotalPadding() with LAME = %d, want %d", got, expected)
	}

	// With LAME info, padding < decoder delay
	info = &Info{
		LAMEVersion:    "LAME3.100",
		EncoderPadding: 100, // Less than DecoderDelay
	}
	if got := info.TotalPadding(); got != 0 {
		t.Errorf("TotalPadding() with small padding = %d, want 0", got)
	}
}

func TestParse_NoXingHeader(t *testing.T) {
	// Frame without Xing/Info tag
	header := []byte{0xFF, 0xFB, 0x90, 0x00}
	sideInfo := make([]byte, 32)
	noTag := []byte("XXXX") // Not Xing or Info
	frame := make([]byte, 0, 500)
	frame = append(frame, header...)
	frame = append(frame, sideInfo...)
	frame = append(frame, noTag...)
	frame = append(frame, make([]byte, 400)...)

	_, err := Parse(frame)
	if !errors.Is(err, ErrNoXingHeader) {
		t.Errorf("Parse() error = %v, want ErrNoXingHeader", err)
	}
}

func TestParse_TooShort(t *testing.T) {
	// Frame too short
	_, err := Parse([]byte{0xFF, 0xFB})
	if !errors.Is(err, ErrNoXingHeader) {
		t.Errorf("Parse() error = %v, want ErrNoXingHeader", err)
	}
}

func TestParse_InvalidSync(t *testing.T) {
	// Invalid sync word
	frame := make([]byte, 100)
	frame[0] = 0x00 // Invalid sync
	_, err := Parse(frame)
	if !errors.Is(err, ErrNoXingHeader) {
		t.Errorf("Parse() error = %v, want ErrNoXingHeader", err)
	}
}

func TestParseFromReader(t *testing.T) {
	frame := buildTestFrame(testFrameOptions{
		isXing:         true,
		flags:          FlagFrameCount | FlagByteCount,
		frameCount:     1234,
		byteCount:      567890,
		lameVersion:    "LAME3.99",
		encoderDelay:   576,
		encoderPadding: 1152,
	})

	r := bytes.NewReader(frame)
	info, err := ParseFromReader(r)
	if err != nil {
		t.Fatalf("ParseFromReader() error = %v", err)
	}

	if info.FrameCount != 1234 {
		t.Errorf("FrameCount = %d, want 1234", info.FrameCount)
	}
	if info.ByteCount != 567890 {
		t.Errorf("ByteCount = %d, want 567890", info.ByteCount)
	}
	if info.LAMEVersion != "LAME3.99\x00" {
		t.Errorf("LAMEVersion = %q, want %q", info.LAMEVersion, "LAME3.99\x00")
	}
	if info.EncoderDelay != 576 {
		t.Errorf("EncoderDelay = %d, want 576", info.EncoderDelay)
	}
	if info.EncoderPadding != 1152 {
		t.Errorf("EncoderPadding = %d, want 1152", info.EncoderPadding)
	}
}

func TestParse_MPEG2Mono(t *testing.T) {
	// Build MPEG2 Layer III mono frame header
	// Sync: 0xFFE (11 bits)
	// Version: 10 (MPEG2)
	// Layer: 01 (Layer III)
	// Protection: 1 (no CRC)
	// Bitrate: 0101 (32kbps for MPEG2 Layer III)
	// Sampling: 00 (22050 Hz)
	// Padding: 0
	// Private: 0
	// Channel: 11 (mono)
	// Mode ext: 00
	// Copyright: 0
	// Original: 0
	// Emphasis: 00
	header := []byte{0xFF, 0xF3, 0x50, 0xC0}

	// Side info for MPEG2 mono = 9 bytes
	sideInfo := make([]byte, 9)

	frame := make([]byte, 0, 300)
	frame = append(frame, header...)
	frame = append(frame, sideInfo...)
	frame = append(frame, []byte("Info")...)
	frame = append(frame, []byte{0, 0, 0, FlagFrameCount}...) // Flags
	frame = append(frame, []byte{0, 0, 0x03, 0xE8}...)        // Frame count = 1000
	frame = append(frame, make([]byte, 200)...)               // Padding

	info, err := Parse(frame)
	if err != nil {
		t.Fatalf("Parse() MPEG2 mono error = %v", err)
	}

	if info.FrameCount != 1000 {
		t.Errorf("FrameCount = %d, want 1000", info.FrameCount)
	}
}

func TestIsLAMEVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"LAME3.100", true},
		{"LAME3.99", true},
		{"L3.99abc", true},
		{"Gogo3dex", true},
		{"GOGO    ", true},
		{"XXXXXXXX", false},
		{"LAM", false}, // Too short
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := isLAMEVersion(tt.version); got != tt.want {
				t.Errorf("isLAMEVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestEncoderDelayPaddingBitPacking(t *testing.T) {
	// Test various delay/padding values to ensure bit packing works correctly
	tests := []struct {
		delay   uint16
		padding uint16
	}{
		{0, 0},
		{576, 1848},
		{576, 0},
		{0, 1152},
		{4095, 4095}, // Max values (12 bits)
		{1, 1},
		{256, 512},
		{2048, 2048},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			frame := buildTestFrame(testFrameOptions{
				isXing:         true,
				flags:          0,
				lameVersion:    "LAME3.100",
				encoderDelay:   tt.delay,
				encoderPadding: tt.padding,
			})

			info, err := Parse(frame)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if info.EncoderDelay != tt.delay {
				t.Errorf("EncoderDelay = %d, want %d", info.EncoderDelay, tt.delay)
			}
			if info.EncoderPadding != tt.padding {
				t.Errorf("EncoderPadding = %d, want %d", info.EncoderPadding, tt.padding)
			}
		})
	}
}

// Integration tests using real LAME-encoded MP3 files

func TestParse_RealLAMEFile(t *testing.T) {
	// This file was encoded with: lame -V2
	path := filepath.Join("..", "example", "classic_lame.mp3")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	info, err := ParseFromReader(f)
	if err != nil {
		t.Fatalf("ParseFromReader() error = %v", err)
	}

	// Verify it's a Xing header (VBR)
	if !info.IsXing {
		t.Error("IsXing = false, want true (file is VBR)")
	}

	// Verify all flags are present (LAME -V2 sets all flags)
	if !info.HasFrameCount() {
		t.Error("HasFrameCount() = false, want true")
	}
	if !info.HasByteCount() {
		t.Error("HasByteCount() = false, want true")
	}
	if !info.HasTOC() {
		t.Error("HasTOC() = false, want true")
	}
	if !info.HasVBRScale() {
		t.Error("HasVBRScale() = false, want true")
	}

	// Verify frame count is reasonable (10 seconds at 44100 Hz ≈ 383 frames)
	// Each frame is 1152 samples, so 10s * 44100 / 1152 ≈ 383
	if info.FrameCount < 300 || info.FrameCount > 500 {
		t.Errorf("FrameCount = %d, want ~384 for 10 second file", info.FrameCount)
	}

	// Verify byte count matches file size (approximately)
	stat, _ := os.Stat(path)
	fileSize := stat.Size()
	// Byte count should be close to file size (minus ID3 tags if any)
	if int64(info.ByteCount) < fileSize/2 || int64(info.ByteCount) > fileSize {
		t.Errorf("ByteCount = %d, file size = %d", info.ByteCount, fileSize)
	}

	// Verify LAME info is present
	if !info.HasLAMEInfo() {
		t.Fatal("HasLAMEInfo() = false, want true")
	}

	// Verify LAME version string
	if info.LAMEVersion != "LAME3.100" {
		t.Errorf("LAMEVersion = %q, want %q", info.LAMEVersion, "LAME3.100")
	}

	// Verify encoder delay is typical LAME value (576)
	if info.EncoderDelay != 576 {
		t.Errorf("EncoderDelay = %d, want 576", info.EncoderDelay)
	}

	// Verify encoder padding is reasonable (should be > 0 and < 2000)
	if info.EncoderPadding == 0 || info.EncoderPadding > 2000 {
		t.Errorf("EncoderPadding = %d, want reasonable value (1-2000)", info.EncoderPadding)
	}

	// Verify TotalDelay calculation
	expectedDelay := int(info.EncoderDelay) + DecoderDelay
	if info.TotalDelay() != expectedDelay {
		t.Errorf("TotalDelay() = %d, want %d", info.TotalDelay(), expectedDelay)
	}

	// Verify VBR scale is reasonable (0-100)
	if info.VBRScale > 100 {
		t.Errorf("VBRScale = %d, want 0-100", info.VBRScale)
	}

	t.Logf("Parsed LAME file successfully:")
	t.Logf("  Frame count: %d", info.FrameCount)
	t.Logf("  Byte count: %d", info.ByteCount)
	t.Logf("  VBR scale: %d", info.VBRScale)
	t.Logf("  LAME version: %s", info.LAMEVersion)
	t.Logf("  Encoder delay: %d", info.EncoderDelay)
	t.Logf("  Encoder padding: %d", info.EncoderPadding)
	t.Logf("  Total delay: %d samples", info.TotalDelay())
	t.Logf("  Total padding: %d samples", info.TotalPadding())
}

func TestParse_RealFileWithoutLAME(t *testing.T) {
	// This file was not encoded with LAME
	path := filepath.Join("..", "example", "classic.mp3")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	_, err = ParseFromReader(f)
	if !errors.Is(err, ErrNoXingHeader) {
		t.Errorf("ParseFromReader() error = %v, want ErrNoXingHeader", err)
	}
}

func TestParse_RealMPEG2File(t *testing.T) {
	// MPEG2 file without LAME header
	path := filepath.Join("..", "example", "mpeg2.mp3")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer f.Close()

	_, err = ParseFromReader(f)
	if !errors.Is(err, ErrNoXingHeader) {
		t.Errorf("ParseFromReader() error = %v, want ErrNoXingHeader", err)
	}
}
