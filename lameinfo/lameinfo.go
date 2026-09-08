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

	"github.com/llehouerou/go-mp3/internal/frameheader"
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
	h := frameheader.FrameHeader(binary.BigEndian.Uint32(frame))
	if !h.IsValid() {
		return nil, ErrNoXingHeader
	}

	// The tag sits right after the side information.
	offset := 4 + h.SideInfoSize()
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
	// Some other encoders also put their name here:
	// - Gogo/GOGO: another LAME-based encoder
	// - Lavc: ffmpeg/libavcodec encoder
	prefix := s[:4]
	return prefix == "LAME" || prefix == "L3.9" || prefix == "Gogo" || prefix == "GOGO" || prefix == "Lavc"
}

// ParseFromReader reads the first MP3 frame from a reader and parses the LAME/Xing header.
// The reader should be positioned at the start of an MP3 frame (after any ID3 tags).
//
// This is a convenience function that reads enough data to parse the header.
// For more control, use Parse with a pre-read frame.
func ParseFromReader(r io.Reader) (*Info, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	h := frameheader.FrameHeader(binary.BigEndian.Uint32(header))
	if !h.IsValid() {
		return nil, ErrNoXingHeader
	}
	size, err := h.FrameSize()
	if err != nil || size < 4 {
		return nil, ErrNoXingHeader
	}
	frame := make([]byte, size)
	copy(frame, header)
	if _, err := io.ReadFull(r, frame[4:]); err != nil {
		return nil, err
	}
	return Parse(frame)
}
