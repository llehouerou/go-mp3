package frameheader

import (
	"errors"
	"io"
	"testing"
	"time"
)

// createTestHeader creates a valid MPEG1 Layer3 frame header for testing.
// The header format is: sync(11) + id(2) + layer(2) + protection(1) + bitrate(4) + freq(2) + ...
func createMPEG1Header(samplingFreqIndex int) FrameHeader {
	// Sync word (11 bits all 1s) = 0xFFE
	// ID = 11 (MPEG1 = Version1)
	// Layer = 01 (Layer3)
	// Protection = 1 (no CRC)
	// Bitrate index = 1001 (128kbps for MPEG1 Layer3)
	// Sampling freq = varies (00=44100, 01=48000, 10=32000)
	// Padding = 0, Private = 0, Mode = 00 (Stereo), ...

	// Base: 0xFFFB9000 for 44100Hz MPEG1 Layer3
	// 1111 1111 1111 1011 1001 xx00 0000 0000
	//                          ^^ sampling freq bits (10,11)
	base := uint32(0xFFFB9000)
	//nolint:gosec // samplingFreqIndex is masked to 2 bits, safe for uint32
	base |= uint32(samplingFreqIndex&0x3) << 10
	return FrameHeader(base)
}

func createMPEG2Header(samplingFreqIndex int) FrameHeader {
	// MPEG2: ID = 10 (Version2)
	// 0xFFF3 instead of 0xFFFB
	base := uint32(0xFFF39000)
	//nolint:gosec // samplingFreqIndex is masked to 2 bits, safe for uint32
	base |= uint32(samplingFreqIndex&0x3) << 10
	return FrameHeader(base)
}

func TestSamplesPerFrame_MPEG1(t *testing.T) {
	h := createMPEG1Header(0) // 44100Hz
	got := h.SamplesPerFrame()
	want := 1152 // SamplesPerGr(576) * Granules(2)
	if got != want {
		t.Errorf("SamplesPerFrame() for MPEG1 = %d, want %d", got, want)
	}
}

func TestSamplesPerFrame_MPEG2(t *testing.T) {
	h := createMPEG2Header(0) // 22050Hz
	got := h.SamplesPerFrame()
	want := 576 // SamplesPerGr(576) * Granules(1)
	if got != want {
		t.Errorf("SamplesPerFrame() for MPEG2 = %d, want %d", got, want)
	}
}

func TestFrameDuration_MPEG1_44100(t *testing.T) {
	h := createMPEG1Header(0) // 44100Hz
	got := h.FrameDuration()
	// 1152 samples / 44100 Hz = 0.026122448... seconds = ~26.122ms
	samples := 1152
	sampleRate := 44100
	want := time.Duration(int64(time.Second) * int64(samples) / int64(sampleRate))
	// Allow small tolerance for floating point
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("FrameDuration() for MPEG1 44100Hz = %v, want %v", got, want)
	}
}

func TestFrameDuration_MPEG1_48000(t *testing.T) {
	h := createMPEG1Header(1) // 48000Hz
	got := h.FrameDuration()
	// 1152 samples / 48000 Hz = 0.024 seconds = 24ms exactly
	want := 24 * time.Millisecond
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("FrameDuration() for MPEG1 48000Hz = %v, want %v", got, want)
	}
}

func TestFrameDuration_MPEG2_22050(t *testing.T) {
	h := createMPEG2Header(0) // 22050Hz (44100 >> 1)
	got := h.FrameDuration()
	// 576 samples / 22050 Hz = 0.026122448... seconds
	samples := 576
	sampleRate := 22050
	want := time.Duration(int64(time.Second) * int64(samples) / int64(sampleRate))
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("FrameDuration() for MPEG2 22050Hz = %v, want %v", got, want)
	}
}

func TestBytesPerSecond_44100(t *testing.T) {
	h := createMPEG1Header(0) // 44100Hz
	got := h.BytesPerSecond()
	want := 44100 * 4 // stereo 16-bit = 4 bytes per sample
	if got != want {
		t.Errorf("BytesPerSecond() for 44100Hz = %d, want %d", got, want)
	}
}

func TestBytesPerSecond_48000(t *testing.T) {
	h := createMPEG1Header(1) // 48000Hz
	got := h.BytesPerSecond()
	want := 48000 * 4
	if got != want {
		t.Errorf("BytesPerSecond() for 48000Hz = %d, want %d", got, want)
	}
}

func TestBytesPerSecond_32000(t *testing.T) {
	h := createMPEG1Header(2) // 32000Hz
	got := h.BytesPerSecond()
	want := 32000 * 4
	if got != want {
		t.Errorf("BytesPerSecond() for 32000Hz = %d, want %d", got, want)
	}
}

func TestBytesPerSecond_MPEG2_22050(t *testing.T) {
	h := createMPEG2Header(0) // 22050Hz
	got := h.BytesPerSecond()
	want := 22050 * 4
	if got != want {
		t.Errorf("BytesPerSecond() for MPEG2 22050Hz = %d, want %d", got, want)
	}
}

// mockReader implements FullReader for testing
type mockReader struct {
	data []byte
	pos  int
}

func (m *mockReader) ReadFull(buf []byte) (int, error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(buf, m.data[m.pos:])
	m.pos += n
	if n < len(buf) {
		return n, io.EOF
	}
	return n, nil
}

func TestRead_SyncSearchLimit(t *testing.T) {
	// Create data larger than MaxSyncSearchBytes (64KB) with no valid frame header.
	// Use 0x00 bytes which will never form a valid sync word (0xFFE).
	dataSize := 70000 // > 64KB
	data := make([]byte, dataSize)
	// Fill with zeros - no valid sync pattern possible

	reader := &mockReader{data: data}
	_, _, err := Read(reader, 0)

	if err == nil {
		t.Fatal("Read() should return error when sync search limit exceeded")
	}

	// Check that we get the specific sync limit error
	var syncErr *SyncSearchLimitError
	if !errors.As(err, &syncErr) {
		t.Errorf("Read() error = %v, want *SyncSearchLimitError", err)
	}
}

func TestRead_ValidHeaderWithinLimit(t *testing.T) {
	// Create data with a valid header after some junk, but within the limit
	junkSize := 1000 // Well within 64KB limit
	data := make([]byte, junkSize+4)

	// Put a valid MPEG1 Layer3 header at position junkSize
	// 0xFFFB9000 is a valid header (see createMPEG1Header)
	validHeader := uint32(0xFFFB9044) // Valid MPEG1 Layer3 128kbps 44100Hz stereo
	data[junkSize] = byte(validHeader >> 24)
	data[junkSize+1] = byte(validHeader >> 16)
	data[junkSize+2] = byte(validHeader >> 8)
	data[junkSize+3] = byte(validHeader)

	reader := &mockReader{data: data}
	header, pos, err := Read(reader, 0)

	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}
	if pos != int64(junkSize) {
		t.Errorf("Read() position = %d, want %d", pos, junkSize)
	}
	if !header.IsValid() {
		t.Error("Read() returned invalid header")
	}
}

func TestIsValid_RejectsNonLayer3(t *testing.T) {
	// This library only supports MP3 (MPEG Layer 3).
	// Headers with Layer 1 or Layer 2 should be rejected by IsValid()
	// to prevent false sync detection on non-MP3 data.

	tests := []struct {
		name   string
		header FrameHeader
		want   bool
	}{
		{
			name:   "Layer3 MPEG1 is valid",
			header: FrameHeader(0xFFFB9044), // Layer bits = 01 (Layer3)
			want:   true,
		},
		{
			name:   "Layer1 MPEG1 is invalid",
			header: FrameHeader(0xFFFF9044), // Layer bits = 11 (Layer1)
			want:   false,
		},
		{
			name:   "Layer2 MPEG1 is invalid",
			header: FrameHeader(0xFFFD9044), // Layer bits = 10 (Layer2)
			want:   false,
		},
		{
			name:   "False sync 0xFFFFC420 (Layer1) is invalid",
			header: FrameHeader(0xFFFFC420), // The actual false sync from Sleep Away.mp3
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.header.IsValid()
			if got != tt.want {
				t.Errorf("IsValid() = %v, want %v (header: 0x%08X)", got, tt.want, tt.header)
			}
		})
	}
}

func TestRead_SkipsNonLayer3Headers(t *testing.T) {
	// Simulate the "Sleep Away.mp3" scenario:
	// Data contains a Layer1 false sync followed by valid Layer3 audio.
	// The decoder should skip the Layer1 header and find the Layer3 header.

	layer1Offset := 100
	layer3Offset := 200

	data := make([]byte, layer3Offset+4)

	// Put a Layer1 header (false sync) at offset 100
	// 0xFFFFC420 is the actual false sync from Sleep Away.mp3
	layer1Header := uint32(0xFFFFC420)
	data[layer1Offset] = byte(layer1Header >> 24)
	data[layer1Offset+1] = byte(layer1Header >> 16)
	data[layer1Offset+2] = byte(layer1Header >> 8)
	data[layer1Offset+3] = byte(layer1Header)

	// Put a valid Layer3 header at offset 200
	layer3Header := uint32(0xFFFBB200) // The actual Layer3 header from Sleep Away.mp3
	data[layer3Offset] = byte(layer3Header >> 24)
	data[layer3Offset+1] = byte(layer3Header >> 16)
	data[layer3Offset+2] = byte(layer3Header >> 8)
	data[layer3Offset+3] = byte(layer3Header)

	reader := &mockReader{data: data}
	header, pos, err := Read(reader, 0)

	if err != nil {
		t.Fatalf("Read() unexpected error: %v", err)
	}
	// Should find the Layer3 header, not the Layer1 header
	if pos != int64(layer3Offset) {
		t.Errorf("Read() position = %d, want %d (should skip Layer1 false sync)", pos, layer3Offset)
	}
	if uint32(header) != layer3Header {
		t.Errorf("Read() header = 0x%08X, want 0x%08X", header, layer3Header)
	}
}
