package frameheader

import (
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
