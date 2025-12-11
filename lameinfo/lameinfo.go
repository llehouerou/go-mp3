// Package lameinfo provides parsing for LAME/Xing VBR headers in MP3 files.
//
// MP3 files encoded with LAME (and some other encoders) contain a special
// info tag in the first frame that provides metadata about the encoding,
// including encoder delay and padding values needed for gapless playback.
//
// The encoder delay indicates how many samples of silence were added at the
// start of the audio, and the encoder padding indicates how many samples
// were added at the end. Decoders can use these values to skip the silent
// samples and achieve sample-accurate playback.
package lameinfo

import (
	"encoding/binary"
	"errors"
	"io"
)

// Info contains the parsed LAME/Xing header information.
type Info struct {
	// IsXing is true if the tag identifier was "Xing" (VBR), false if "Info" (CBR).
	IsXing bool

	// Flags indicates which optional fields are present.
	Flags uint32

	// FrameCount is the total number of MP3 frames (if HasFrameCount is true).
	FrameCount uint32

	// ByteCount is the total size of the audio stream in bytes (if HasByteCount is true).
	ByteCount uint32

	// TOC is the seek table with 100 entries for VBR seeking (if HasTOC is true).
	// Each entry is a percentage (0-255) of the file position for that percentage of playback.
	TOC [100]byte

	// VBRScale is the VBR quality indicator 0-100 (if HasVBRScale is true).
	VBRScale uint32

	// LAMEVersion is the encoder version string (e.g., "LAME3.100").
	// Empty if no LAME tag is present.
	LAMEVersion string

	// EncoderDelay is the number of samples added at the start by the encoder.
	// Typically 576 for LAME. Valid only if HasLAMEInfo is true.
	EncoderDelay uint16

	// EncoderPadding is the number of samples added at the end by the encoder.
	// Valid only if HasLAMEInfo is true.
	EncoderPadding uint16
}

// Flag constants for the Flags field.
const (
	FlagFrameCount = 0x0001
	FlagByteCount  = 0x0002
	FlagTOC        = 0x0004
	FlagVBRScale   = 0x0008
)

// HasFrameCount returns true if the frame count field is present.
func (i *Info) HasFrameCount() bool {
	return i.Flags&FlagFrameCount != 0
}

// HasByteCount returns true if the byte count field is present.
func (i *Info) HasByteCount() bool {
	return i.Flags&FlagByteCount != 0
}

// HasTOC returns true if the TOC (seek table) is present.
func (i *Info) HasTOC() bool {
	return i.Flags&FlagTOC != 0
}

// HasVBRScale returns true if the VBR scale field is present.
func (i *Info) HasVBRScale() bool {
	return i.Flags&FlagVBRScale != 0
}

// HasLAMEInfo returns true if LAME-specific info (encoder delay/padding) is present.
func (i *Info) HasLAMEInfo() bool {
	return i.LAMEVersion != ""
}

// DecoderDelay is the standard decoder delay for MP3 decoders (529 samples).
// This is in addition to the encoder delay stored in the LAME tag.
const DecoderDelay = 529

// TotalDelay returns the total number of samples to skip at the start
// for gapless playback (encoder delay + decoder delay).
func (i *Info) TotalDelay() int {
	if !i.HasLAMEInfo() {
		return DecoderDelay
	}
	return int(i.EncoderDelay) + DecoderDelay
}

// TotalPadding returns the number of samples to trim from the end
// for gapless playback, accounting for decoder delay.
func (i *Info) TotalPadding() int {
	if !i.HasLAMEInfo() {
		return 0
	}
	// The padding value already accounts for what needs to be trimmed
	padding := int(i.EncoderPadding) - DecoderDelay
	if padding < 0 {
		return 0
	}
	return padding
}

// ErrNoXingHeader is returned when no Xing/Info header is found.
var ErrNoXingHeader = errors.New("lameinfo: no Xing/Info header found")

// sideInfoSize returns the size of the side information based on
// MPEG version and channel mode.
func sideInfoSize(mpegVersion int, mono bool) int {
	if mpegVersion == 1 { // MPEG1
		if mono {
			return 17
		}
		return 32
	}
	// MPEG2 or MPEG2.5
	if mono {
		return 9
	}
	return 17
}

// Parse reads an MP3 frame and extracts LAME/Xing header information.
// The frame should be the first audio frame of the MP3 file (after any ID3 tags).
//
// The frame parameter should contain the complete first MP3 frame including
// the 4-byte frame header.
//
// Returns ErrNoXingHeader if no Xing/Info tag is found in the frame.
func Parse(frame []byte) (*Info, error) {
	if len(frame) < 4 {
		return nil, ErrNoXingHeader
	}

	// Parse frame header to determine side info size
	header := binary.BigEndian.Uint32(frame[0:4])

	// Check sync word (11 bits)
	if (header & 0xFFE00000) != 0xFFE00000 {
		return nil, ErrNoXingHeader
	}

	// Extract MPEG version (bits 19-20)
	mpegVersion := int((header >> 19) & 0x03)
	if mpegVersion == 1 { // Reserved
		return nil, ErrNoXingHeader
	}
	// Convert: 0=2.5, 2=2, 3=1
	var version int
	switch mpegVersion {
	case 0:
		version = 25 // MPEG 2.5
	case 2:
		version = 2 // MPEG 2
	case 3:
		version = 1 // MPEG 1
	}

	// Extract channel mode (bits 6-7)
	channelMode := (header >> 6) & 0x03
	mono := channelMode == 3 // 3 = single channel (mono)

	// Calculate offset to Xing tag
	var sideInfo int
	if version == 1 {
		sideInfo = sideInfoSize(1, mono)
	} else {
		sideInfo = sideInfoSize(2, mono)
	}

	offset := 4 + sideInfo // 4-byte header + side info

	// Check for Xing or Info tag
	if len(frame) < offset+4 {
		return nil, ErrNoXingHeader
	}

	tag := string(frame[offset : offset+4])
	if tag != "Xing" && tag != "Info" {
		return nil, ErrNoXingHeader
	}

	info := &Info{
		IsXing: tag == "Xing",
	}

	pos := offset + 4

	// Read flags
	if len(frame) < pos+4 {
		return nil, ErrNoXingHeader
	}
	info.Flags = binary.BigEndian.Uint32(frame[pos : pos+4])
	pos += 4

	// Read optional fields based on flags
	if info.HasFrameCount() {
		if len(frame) < pos+4 {
			return nil, ErrNoXingHeader
		}
		info.FrameCount = binary.BigEndian.Uint32(frame[pos : pos+4])
		pos += 4
	}

	if info.HasByteCount() {
		if len(frame) < pos+4 {
			return nil, ErrNoXingHeader
		}
		info.ByteCount = binary.BigEndian.Uint32(frame[pos : pos+4])
		pos += 4
	}

	if info.HasTOC() {
		if len(frame) < pos+100 {
			return nil, ErrNoXingHeader
		}
		copy(info.TOC[:], frame[pos:pos+100])
		pos += 100
	}

	if info.HasVBRScale() {
		if len(frame) < pos+4 {
			return nil, ErrNoXingHeader
		}
		info.VBRScale = binary.BigEndian.Uint32(frame[pos : pos+4])
		pos += 4
	}

	// Try to read LAME tag (9-byte version string)
	if len(frame) >= pos+9 {
		version := string(frame[pos : pos+9])
		// Check if it looks like a LAME version string
		if isLAMEVersion(version) {
			info.LAMEVersion = version
			pos += 9

			// Skip to encoder delay/padding (21 bytes after version string)
			// Layout after version:
			// 1 byte: revision/VBR method
			// 1 byte: lowpass filter
			// 4 bytes: peak signal
			// 2 bytes: radio replay gain
			// 2 bytes: audiophile replay gain
			// 1 byte: encoding flags
			// 1 byte: ABR/minimal bitrate
			// = 12 bytes, then 3 bytes for delay/padding
			delayOffset := pos + 12

			if len(frame) >= delayOffset+3 {
				// Encoder delay: 12 bits
				// Byte 0: upper 8 bits of delay
				// Byte 1: lower 4 bits of delay (upper nibble) | upper 4 bits of padding (lower nibble)
				// Byte 2: lower 8 bits of padding
				info.EncoderDelay = uint16(frame[delayOffset])<<4 | uint16(frame[delayOffset+1])>>4
				info.EncoderPadding = uint16(frame[delayOffset+1]&0x0F)<<8 | uint16(frame[delayOffset+2])
			}
		}
	}

	return info, nil
}

// isLAMEVersion checks if the string looks like a LAME version identifier.
func isLAMEVersion(s string) bool {
	if len(s) < 4 {
		return false
	}
	// LAME versions start with "LAME" or "L3.9" (older format)
	// Some other encoders also put their name here
	prefix := s[:4]
	return prefix == "LAME" || prefix == "L3.9" || prefix == "Gogo" || prefix == "GOGO"
}

// ParseFromReader reads the first MP3 frame from a reader and parses the LAME/Xing header.
// The reader should be positioned at the start of an MP3 frame (after any ID3 tags).
//
// This is a convenience function that reads enough data to parse the header.
// For more control, use Parse with a pre-read frame.
func ParseFromReader(r io.Reader) (*Info, error) {
	// Read the frame header first
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	// Parse header to get frame size
	h := binary.BigEndian.Uint32(header)

	// Check sync word
	if (h & 0xFFE00000) != 0xFFE00000 {
		return nil, ErrNoXingHeader
	}

	// Extract fields needed to calculate frame size
	mpegVersion := (h >> 19) & 0x03
	layer := (h >> 17) & 0x03
	bitrateIndex := (h >> 12) & 0x0F
	samplingRateIndex := (h >> 10) & 0x03
	padding := (h >> 9) & 0x01

	if mpegVersion == 1 || layer == 0 || bitrateIndex == 0 || bitrateIndex == 15 || samplingRateIndex == 3 {
		return nil, ErrNoXingHeader
	}

	// Calculate frame size
	frameSize := calculateFrameSize(mpegVersion, layer, bitrateIndex, samplingRateIndex, padding)
	if frameSize < 4 {
		return nil, ErrNoXingHeader
	}

	// Read the rest of the frame
	frame := make([]byte, frameSize)
	copy(frame, header)
	if _, err := io.ReadFull(r, frame[4:]); err != nil {
		return nil, err
	}

	return Parse(frame)
}

// Bitrate tables for frame size calculation
var bitrateTable = [4][4][16]int{
	// MPEG 2.5 (index 0)
	{
		{0}, // Reserved
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},      // Layer III
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},      // Layer II
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0}, // Layer I
	},
	// Reserved (index 1)
	{},
	// MPEG 2 (index 2)
	{
		{0}, // Reserved
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},      // Layer III
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},      // Layer II
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0}, // Layer I
	},
	// MPEG 1 (index 3)
	{
		{0}, // Reserved
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},     // Layer III
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},    // Layer II
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0}, // Layer I
	},
}

var samplingRateTable = [4][4]int{
	{11025, 12000, 8000, 0},  // MPEG 2.5
	{0, 0, 0, 0},             // Reserved
	{22050, 24000, 16000, 0}, // MPEG 2
	{44100, 48000, 32000, 0}, // MPEG 1
}

func calculateFrameSize(mpegVersion, layer, bitrateIndex, samplingRateIndex, padding uint32) int {
	bitrate := bitrateTable[mpegVersion][layer][bitrateIndex] * 1000
	samplingRate := samplingRateTable[mpegVersion][samplingRateIndex]

	if bitrate == 0 || samplingRate == 0 {
		return 0
	}

	var frameSize int
	if layer == 3 { // Layer I
		frameSize = (12*bitrate/samplingRate + int(padding)) * 4
	} else { // Layer II or III
		if mpegVersion == 3 { // MPEG 1
			frameSize = 144*bitrate/samplingRate + int(padding)
		} else { // MPEG 2 or 2.5
			frameSize = 72*bitrate/samplingRate + int(padding)
		}
	}

	return frameSize
}
