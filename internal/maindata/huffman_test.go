package maindata

import (
	"testing"

	"github.com/llehouerou/go-mp3/internal/bits"
	"github.com/llehouerou/go-mp3/internal/frameheader"
	"github.com/llehouerou/go-mp3/internal/sideinfo"
)

// TestReadHuffmanRegionCountOverflow tests that readHuffman handles the case where
// Region0Count + Region1Count + 2 exceeds the scalefactor band table bounds.
// This matches the behavior of mpg123 and ffmpeg which clamp the index.
func TestReadHuffmanRegionCountOverflow(t *testing.T) {
	// Create a minimal bit buffer with some data for Huffman decoding
	// The actual content doesn't matter much since we're testing the region bounds check
	data := make([]byte, 256)
	m := bits.New(data)

	// Create a mock frame header for MPEG1 Layer 3, 44100Hz stereo
	// We need a header that returns the correct values for the test
	header := createTestFrameHeader()

	// Create side info with Region0Count=15 and Region1Count=7
	// This gives j = 15 + 7 + 2 = 24, which exceeds the table bounds (max index 22)
	si := &sideinfo.SideInfo{
		Part2_3Length:    [2][2]int{{100, 100}, {100, 100}},
		BigValues:        [2][2]int{{10, 10}, {10, 10}}, // Small value to limit iterations
		WinSwitchFlag:    [2][2]int{{0, 0}, {0, 0}},     // Long blocks (no window switch)
		BlockType:        [2][2]int{{0, 0}, {0, 0}},
		Region0Count:     [2][2]int{{15, 15}, {15, 15}}, // Max value (4 bits)
		Region1Count:     [2][2]int{{7, 7}, {7, 7}},     // Max value (3 bits)
		TableSelect:      [2][2][3]int{{{0, 0, 0}, {0, 0, 0}}, {{0, 0, 0}, {0, 0, 0}}},
		GlobalGain:       [2][2]int{{200, 200}, {200, 200}},
		ScalefacCompress: [2][2]int{{0, 0}, {0, 0}},
	}

	mainData := &MainData{}

	// This should NOT return an error - it should clamp the region2Start
	// to SamplesPerGr (576) like mpg123 and ffmpeg do
	err := readHuffman(m, header, si, mainData, 0, 0, 0)
	if err != nil {
		t.Errorf("readHuffman should handle Region0Count+Region1Count+2 > 22 by clamping, got error: %v", err)
	}
}

// createTestFrameHeader creates a mock frame header for testing
func createTestFrameHeader() frameheader.FrameHeader {
	// MPEG1 Layer 3, 44100Hz, stereo frame header
	// Frame sync (11 bits) + Version (2 bits) + Layer (2 bits) + ...
	// 0xFF 0xFB = 1111 1111 1111 1011 = sync + MPEG1 + Layer3 + no CRC
	// 0x90 = 1001 0000 = 128kbps + 44100Hz + no padding + private
	// 0x00 = stereo mode
	headerBytes := []byte{0xFF, 0xFB, 0x90, 0x00}
	h, _, _ := frameheader.Read(&mockReader{data: headerBytes}, 0)
	return h
}

type mockReader struct {
	data []byte
	pos  int
}

func (r *mockReader) ReadFull(buf []byte) (int, error) {
	n := copy(buf, r.data[r.pos:])
	r.pos += n
	return n, nil
}
