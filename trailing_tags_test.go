package mp3

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/llehouerou/go-mp3/internal/frameheader"
)

// createMinimalMP3Frame creates a minimal valid MPEG1 Layer3 frame for testing.
// This creates a 417-byte frame (128kbps, 44100Hz, no padding).
func createMinimalMP3Frame() []byte {
	// MPEG1 Layer3 128kbps 44100Hz stereo frame
	// Frame size = 144 * bitrate / samplerate + padding = 144 * 128000 / 44100 = 417 bytes
	frame := make([]byte, 417)

	// Frame header: 0xFFFB9044
	// - Sync word: 0xFFF
	// - ID: 11 (MPEG1)
	// - Layer: 01 (Layer3)
	// - Protection: 1 (no CRC)
	// - Bitrate: 1001 (128kbps)
	// - Sampling freq: 00 (44100Hz)
	// - Padding: 0
	// - Private: 0
	// - Mode: 01 (Joint stereo)
	// - Mode ext: 00
	// - Copyright: 0
	// - Original: 1
	// - Emphasis: 00
	frame[0] = 0xFF
	frame[1] = 0xFB
	frame[2] = 0x90
	frame[3] = 0x44

	// Side info for stereo MPEG1 (32 bytes)
	// Main data begin = 0 (no bit reservoir)
	frame[4] = 0x00
	frame[5] = 0x00
	// Rest of side info (30 bytes) - zeros are acceptable for minimal valid frame

	// Main data - fill with zeros (silence)
	// The rest of the frame is already zero-initialized

	return frame
}

// createAPETagHeader creates an APEv2 tag header for testing.
// APE tags start with "APETAGEX" followed by version and other metadata.
func createAPETagHeader(tagSize uint32) []byte {
	header := make([]byte, 32)

	// Preamble "APETAGEX"
	copy(header[0:8], "APETAGEX")

	// Version (2000 = APEv2)
	header[8] = 0xD0
	header[9] = 0x07
	header[10] = 0x00
	header[11] = 0x00

	// Tag size (excluding header)
	header[12] = byte(tagSize)
	header[13] = byte(tagSize >> 8)
	header[14] = byte(tagSize >> 16)
	header[15] = byte(tagSize >> 24)

	// Item count
	header[16] = 0x01
	header[17] = 0x00
	header[18] = 0x00
	header[19] = 0x00

	// Flags (header present, footer present)
	header[20] = 0xA0
	header[21] = 0x00
	header[22] = 0x00
	header[23] = 0x80

	// Reserved (8 bytes of zeros)

	return header
}

// createID3v1Tag creates a minimal ID3v1 tag for testing.
// ID3v1 tags are exactly 128 bytes and start with "TAG".
func createID3v1Tag() []byte {
	tag := make([]byte, 128)
	copy(tag[0:3], "TAG")
	copy(tag[3:33], "Test Title")
	copy(tag[33:63], "Test Artist")
	copy(tag[63:93], "Test Album")
	copy(tag[93:97], "2024")
	return tag
}

// TestDecoder_WithTrailingAPETag tests that the decoder handles files with APE tags at the end.
func TestDecoder_WithTrailingAPETag(t *testing.T) {
	// Create a minimal MP3 file with multiple frames followed by an APE tag
	var buf bytes.Buffer

	// Write several MP3 frames
	numFrames := 10
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	audioDataEnd := buf.Len()

	// Append APE tag
	apeTagBody := []byte("ALBUM\x00Test Album Name")
	//nolint:gosec // test data is small, no overflow risk
	apeHeader := createAPETagHeader(uint32(len(apeTagBody)))
	buf.Write(apeHeader)
	buf.Write(apeTagBody)

	// Create decoder
	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Verify basic properties
	if d.SampleRate() != 44100 {
		t.Errorf("SampleRate() = %d, want 44100", d.SampleRate())
	}

	// Verify length calculation doesn't include APE tag
	// Each frame produces 4608 bytes of PCM (1152 samples * 4 bytes per sample)
	expectedPCMLength := int64(numFrames * 1152 * 4)
	if d.Length() != expectedPCMLength {
		t.Errorf("Length() = %d, want %d (should not include APE tag)", d.Length(), expectedPCMLength)
	}

	// Try to decode all audio - should succeed without error
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != expectedPCMLength {
		t.Errorf("Decoded %d bytes, want %d", len(pcm), expectedPCMLength)
	}

	t.Logf("Successfully decoded MP3 with trailing APE tag (audio: %d bytes, APE tag at: %d)",
		len(pcm), audioDataEnd)
}

// TestDecoder_WithTrailingID3v1Tag tests that the decoder handles files with ID3v1 tags at the end.
func TestDecoder_WithTrailingID3v1Tag(t *testing.T) {
	// Create a minimal MP3 file with multiple frames followed by an ID3v1 tag
	var buf bytes.Buffer

	// Write several MP3 frames
	numFrames := 10
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	audioDataEnd := buf.Len()

	// Append ID3v1 tag
	id3v1 := createID3v1Tag()
	buf.Write(id3v1)

	// Create decoder
	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Verify length calculation doesn't include ID3v1 tag
	expectedPCMLength := int64(numFrames * 1152 * 4)
	if d.Length() != expectedPCMLength {
		t.Errorf("Length() = %d, want %d (should not include ID3v1 tag)", d.Length(), expectedPCMLength)
	}

	// Try to decode all audio - should succeed without error
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != expectedPCMLength {
		t.Errorf("Decoded %d bytes, want %d", len(pcm), expectedPCMLength)
	}

	t.Logf("Successfully decoded MP3 with trailing ID3v1 tag (audio: %d bytes, ID3v1 at: %d)",
		len(pcm), audioDataEnd)
}

// TestDecoder_WithBothID3v2AndTrailingAPE tests files with ID3v2 at start and APE at end.
func TestDecoder_WithBothID3v2AndTrailingAPE(t *testing.T) {
	var buf bytes.Buffer

	// Write ID3v2 header at the beginning
	// ID3v2 header: "ID3" + version(2) + flags(1) + size(4 syncsafe)
	id3v2Header := []byte{
		'I', 'D', '3', // ID3 identifier
		0x04, 0x00, // Version 2.4.0
		0x00,                   // Flags
		0x00, 0x00, 0x00, 0x10, // Size: 16 bytes (syncsafe)
	}
	buf.Write(id3v2Header)

	// Write 16 bytes of ID3v2 tag body (padding)
	id3v2Body := make([]byte, 16)
	buf.Write(id3v2Body)

	// Write several MP3 frames
	numFrames := 10
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	// Append APE tag
	apeTagBody := []byte("TITLE\x00Test Song Title")
	//nolint:gosec // test data is small, no overflow risk
	apeHeader := createAPETagHeader(uint32(len(apeTagBody)))
	buf.Write(apeHeader)
	buf.Write(apeTagBody)

	// Create decoder
	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Verify basic properties
	if d.SampleRate() != 44100 {
		t.Errorf("SampleRate() = %d, want 44100", d.SampleRate())
	}

	// Verify length
	expectedPCMLength := int64(numFrames * 1152 * 4)
	if d.Length() != expectedPCMLength {
		t.Errorf("Length() = %d, want %d", d.Length(), expectedPCMLength)
	}

	// Try to decode all audio
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != expectedPCMLength {
		t.Errorf("Decoded %d bytes, want %d", len(pcm), expectedPCMLength)
	}

	t.Logf("Successfully decoded MP3 with ID3v2 header and trailing APE tag (audio: %d bytes)", len(pcm))
}

// TestDecoder_WithLargeTrailingGarbage tests that the decoder handles large amounts of
// non-audio data at the end of the file gracefully.
func TestDecoder_WithLargeTrailingGarbage(t *testing.T) {
	var buf bytes.Buffer

	// Write several MP3 frames
	numFrames := 5
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	// Append 100KB of random non-MP3 data (simulating large metadata or corruption)
	garbage := make([]byte, 100*1024)
	for i := range garbage {
		garbage[i] = byte(i % 256)
	}
	// Make sure garbage doesn't accidentally contain valid sync word
	for i := range len(garbage) - 1 {
		if garbage[i] == 0xFF && (garbage[i+1]&0xE0) == 0xE0 {
			garbage[i+1] = 0x00
		}
	}
	buf.Write(garbage)

	// Create decoder
	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Try to decode all audio - should succeed
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	expectedPCMLength := int64(numFrames * 1152 * 4)
	if int64(len(pcm)) != expectedPCMLength {
		t.Errorf("Decoded %d bytes, want %d", len(pcm), expectedPCMLength)
	}

	t.Logf("Successfully decoded MP3 with 100KB trailing garbage (audio: %d bytes)", len(pcm))
}

// TestDecoder_SeekWithTrailingTags tests that seeking works correctly when trailing tags are present.
func TestDecoder_SeekWithTrailingTags(t *testing.T) {
	var buf bytes.Buffer

	// Write several MP3 frames
	numFrames := 20
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	// Append APE tag
	apeTagBody := []byte("ARTIST\x00Test Artist")
	//nolint:gosec // test data is small, no overflow risk
	apeHeader := createAPETagHeader(uint32(len(apeTagBody)))
	buf.Write(apeHeader)
	buf.Write(apeTagBody)

	// Create decoder
	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	totalLength := d.Length()
	if totalLength <= 0 {
		t.Fatalf("Length() = %d, expected positive value", totalLength)
	}

	// Seek to middle
	midpoint := totalLength / 2
	// Align to 4-byte boundary
	midpoint &^= 3

	pos, err := d.Seek(midpoint, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek() failed: %v", err)
	}
	if pos != midpoint {
		t.Errorf("Seek() returned position %d, want %d", pos, midpoint)
	}

	// Read remaining data
	remaining, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() after seek failed: %v", err)
	}

	expectedRemaining := totalLength - midpoint
	if int64(len(remaining)) != expectedRemaining {
		t.Errorf("Read %d bytes after seek, want %d", len(remaining), expectedRemaining)
	}

	// Seek to end should work
	pos, err = d.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek(0, SeekEnd) failed: %v", err)
	}
	if pos != totalLength {
		t.Errorf("Seek(0, SeekEnd) returned %d, want %d", pos, totalLength)
	}

	t.Logf("Seek operations work correctly with trailing tags")
}

// TestDecoder_RealFileWithAPETags tests with a real file that has APE tags if available.
func TestDecoder_RealFileWithAPETags(t *testing.T) {
	// This test uses the Maggie Rogers album files if available
	testFile := "/media/srv02/musique/Maggie Rogers/2024 • Don't Forget Me/Maggie Rogers • Don't Forget Me • 01 · It Was Coming All Along.mp3"

	f, err := os.Open(testFile)
	if err != nil {
		t.Skipf("Test file not available: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	t.Logf("Sample rate: %d Hz", d.SampleRate())
	t.Logf("Duration: %v", d.Duration())
	t.Logf("Length: %d bytes", d.Length())

	// Decode entire file
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != d.Length() {
		t.Errorf("Decoded %d bytes, expected %d", len(pcm), d.Length())
	}

	t.Logf("Successfully decoded %d bytes of PCM audio", len(pcm))
}

// TestSyncSearchLimitError_IsHandledGracefully verifies that SyncSearchLimitError
// is properly handled as end-of-audio in both scanning and reading phases.
func TestSyncSearchLimitError_IsHandledGracefully(t *testing.T) {
	// Create data that will trigger SyncSearchLimitError
	var buf bytes.Buffer

	// Write a few valid frames
	numFrames := 3
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	// Add data that looks like start of sync but isn't valid
	// This ensures we test the actual sync search path
	invalidData := make([]byte, 70000) // > 64KB limit
	// Fill with pattern that might look like partial sync words
	for i := 0; i < len(invalidData); i += 100 {
		invalidData[i] = 0xFF
		if i+1 < len(invalidData) {
			invalidData[i+1] = 0x00 // Invalid second byte (not 0xE0+)
		}
	}
	buf.Write(invalidData)

	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Decoder should have been created successfully
	// Length should reflect only the valid frames
	expectedPCMLength := int64(numFrames * 1152 * 4)
	if d.Length() != expectedPCMLength {
		t.Errorf("Length() = %d, want %d", d.Length(), expectedPCMLength)
	}

	// Reading should work and return EOF at the end, not an error
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != expectedPCMLength {
		t.Errorf("Decoded %d bytes, want %d", len(pcm), expectedPCMLength)
	}
}

// createID3v2Tag creates an ID3v2 tag with the specified version and body size.
// The body is filled with zeros (padding).
func createID3v2Tag(majorVersion byte, bodySize int) []byte {
	// ID3v2 size is stored as syncsafe integer (7 bits per byte)
	syncsafeSize := make([]byte, 4)
	syncsafeSize[0] = byte((bodySize >> 21) & 0x7F)
	syncsafeSize[1] = byte((bodySize >> 14) & 0x7F)
	syncsafeSize[2] = byte((bodySize >> 7) & 0x7F)
	syncsafeSize[3] = byte(bodySize & 0x7F)

	header := []byte{
		'I', 'D', '3', // ID3 identifier
		majorVersion, 0x00, // Version
		0x00, // Flags
		syncsafeSize[0], syncsafeSize[1], syncsafeSize[2], syncsafeSize[3],
	}

	tag := make([]byte, 10+bodySize)
	copy(tag, header)
	return tag
}

// TestDecoder_WithMultipleConsecutiveID3v2Tags tests that the decoder handles files
// with multiple ID3v2 tags at the beginning (e.g., ID3v2.3 followed by ID3v2.4).
// This is a common scenario when files are re-tagged by different software.
func TestDecoder_WithMultipleConsecutiveID3v2Tags(t *testing.T) {
	var buf bytes.Buffer

	// Write first ID3v2.3 tag (like the real Bon Iver file: ~84KB)
	// Using 100KB to ensure it's larger than sync search limit (64KB)
	firstTagSize := 100 * 1024
	buf.Write(createID3v2Tag(0x03, firstTagSize))

	// Write second ID3v2.4 tag (like the real file: ~150KB)
	secondTagSize := 150 * 1024
	buf.Write(createID3v2Tag(0x04, secondTagSize))

	// Write several MP3 frames
	numFrames := 10
	frame := createMinimalMP3Frame()
	for range numFrames {
		buf.Write(frame)
	}

	// Create decoder - should succeed even with multiple ID3 tags
	reader := bytes.NewReader(buf.Bytes())
	d, err := NewDecoder(reader)
	if err != nil {
		t.Fatalf("NewDecoder() failed: %v", err)
	}

	// Verify basic properties
	if d.SampleRate() != 44100 {
		t.Errorf("SampleRate() = %d, want 44100", d.SampleRate())
	}

	// Verify length
	expectedPCMLength := int64(numFrames * 1152 * 4)
	if d.Length() != expectedPCMLength {
		t.Errorf("Length() = %d, want %d", d.Length(), expectedPCMLength)
	}

	// Try to decode all audio
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll() failed: %v", err)
	}

	if int64(len(pcm)) != expectedPCMLength {
		t.Errorf("Decoded %d bytes, want %d", len(pcm), expectedPCMLength)
	}

	t.Logf("Successfully decoded MP3 with two consecutive ID3v2 tags (audio: %d bytes)", len(pcm))
}

// TestSyncSearchLimitError_TypeAssertion verifies the error type can be properly detected.
func TestSyncSearchLimitError_TypeAssertion(t *testing.T) {
	err := &frameheader.SyncSearchLimitError{BytesSearched: 65536}

	// Test errors.As works
	var syncErr *frameheader.SyncSearchLimitError
	if !errors.As(err, &syncErr) {
		t.Error("errors.As failed to match SyncSearchLimitError")
	}

	if syncErr.BytesSearched != 65536 {
		t.Errorf("BytesSearched = %d, want 65536", syncErr.BytesSearched)
	}

	// Test error message
	msg := err.Error()
	if msg != "mp3: no valid frame header found within 65536 bytes" {
		t.Errorf("Error() = %q, want specific message", msg)
	}
}
