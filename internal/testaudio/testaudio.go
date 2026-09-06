// Package testaudio synthesises MP3 files for tests.
//
// Files are built frame by frame from real headers, so the decoder parses them
// exactly as it parses encoder output. Audio payloads are zero-filled: the
// point is the frame structure, not the sound.
package testaudio

import (
	"encoding/binary"
	"fmt"

	"github.com/llehouerou/go-mp3/internal/consts"
	"github.com/llehouerou/go-mp3/internal/frameheader"
	"github.com/llehouerou/go-mp3/lameinfo"
)

// XingMode selects the VBR header written in the first frame.
type XingMode int

const (
	// NoXing writes no VBR header frame at all.
	NoXing XingMode = iota
	// Xing writes a "Xing" header frame (conventionally VBR).
	Xing
	// Info writes an "Info" header frame (conventionally CBR).
	Info
)

// Options describes a file to synthesise.
type Options struct {
	// Version is the MPEG version as 1, 2 or 25; zero means 1.
	Version int
	// Mono selects single-channel mode instead of stereo.
	Mono bool
	// SampleRateIndex is the raw header field: 0=44100, 1=48000, 2=32000,
	// halved for MPEG2 and quartered for MPEG2.5.
	SampleRateIndex int
	// Frames is the number of audio frames, excluding any Xing header frame.
	Frames int
	// BitratesKbps is cycled over the audio frames. A single entry means CBR,
	// several mean VBR. Empty means 128 kbps (64 for MPEG2/2.5).
	BitratesKbps []int

	// XingMode selects the VBR header frame prepended to the audio frames.
	XingMode XingMode
	// XingFrameCount overrides the frame count written in the Xing header.
	// Zero writes the truthful count (Frames).
	XingFrameCount uint32
	// OmitByteCount clears the byte count flag, as many encoders do.
	OmitByteCount bool
	// LAME appends a LAME tag carrying EncoderDelay and EncoderPadding.
	LAME bool
	// EncoderDelay and EncoderPadding are the gapless trim amounts, in samples.
	EncoderDelay   uint16
	EncoderPadding uint16

	// ID3v2Size prepends an ID3v2 tag with that many bytes of payload.
	ID3v2Size int
	// ID3v1 appends a 128-byte TAG block.
	ID3v1 bool
	// TrailingGarbage is appended after the audio (and after any ID3v1 tag).
	TrailingGarbage []byte
	// TruncateBytes chops that many bytes off the end, mid-frame.
	TruncateBytes int
}

// Build synthesises the described file.
func Build(o Options) []byte {
	version, bitrates := o.resolve()

	var out []byte
	if o.ID3v2Size > 0 {
		out = append(out, id3v2Tag(o.ID3v2Size)...)
	}

	audioStart := len(out)
	if o.XingMode != NoXing {
		// Placeholder: the byte count is only known once every frame is built.
		out = append(out, make([]byte, len(o.xingFrame(version, bitrates[0], 0)))...)
	}

	for i := range o.Frames {
		h := header(version, o.Mono, o.SampleRateIndex, bitrates[i%len(bitrates)], false)
		out = append(out, frame(h, nil)...)
	}

	if o.XingMode != NoXing {
		byteCount := uint32(len(out) - audioStart) //nolint:gosec // synthesised files are kilobytes
		copy(out[audioStart:], o.xingFrame(version, bitrates[0], byteCount))
	}

	if o.ID3v1 {
		tag := make([]byte, 128)
		copy(tag, "TAG")
		out = append(out, tag...)
	}
	out = append(out, o.TrailingGarbage...)

	if o.TruncateBytes > 0 {
		if o.TruncateBytes >= len(out) {
			return nil
		}
		out = out[:len(out)-o.TruncateBytes]
	}
	return out
}

// resolve fills in the defaults: MPEG1, and a bitrate typical of the version.
func (o Options) resolve() (version consts.Version, bitratesKbps []int) {
	version = mpegVersion(o.Version)
	if len(o.BitratesKbps) > 0 {
		return version, o.BitratesKbps
	}
	if version == consts.Version1 {
		return version, []int{128}
	}
	return version, []int{64}
}

// FrameSize returns the encoded size of one frame of the described file, which
// is constant only for CBR files.
func FrameSize(o Options) int {
	version, bitrates := o.resolve()
	size, err := header(version, o.Mono, o.SampleRateIndex, bitrates[0], false).FrameSize()
	if err != nil {
		panic(err)
	}
	return size
}

// mpegVersion maps the 1/2/25 spelling used by Options onto the header field.
func mpegVersion(v int) consts.Version {
	switch v {
	case 0, 1:
		return consts.Version1
	case 2:
		return consts.Version2
	case 25:
		return consts.Version2_5
	}
	panic(fmt.Sprintf("testaudio: unknown MPEG version %d", v))
}

// header assembles a frame header from its fields.
func header(version consts.Version, mono bool, sampleRateIndex, bitrateKbps int, padding bool) frameheader.FrameHeader {
	h := frameheader.FrameHeader(0xffe00000)
	h |= field(int(version), 19)
	h |= field(int(consts.Layer3), 17)
	h |= 0x00010000 // protection bit set = no CRC
	h |= field(sampleRateIndex, 10)
	if padding {
		h |= 0x00000200
	}
	mode := consts.ModeStereo
	if mono {
		mode = consts.ModeSingleChannel
	}
	h |= field(int(mode), 6)
	h |= field(bitrateIndex(h, bitrateKbps), 12)

	if !h.IsValid() {
		panic(fmt.Sprintf("testaudio: built an invalid frame header %#08x", uint32(h)))
	}
	return h
}

// field shifts a small non-negative header field into its position.
func field(v, shift int) frameheader.FrameHeader {
	//nolint:gosec // header fields are small non-negative values
	return frameheader.FrameHeader(uint32(v) << shift)
}

// bitrateIndex finds the header field encoding the requested bitrate, reusing
// the decoder's own tables rather than restating them.
func bitrateIndex(h frameheader.FrameHeader, kbps int) int {
	for i := 1; i <= 14; i++ {
		if (h | field(i, 12)).Bitrate() == kbps*1000 {
			return i
		}
	}
	panic(fmt.Sprintf("testaudio: no bitrate index for %d kbps", kbps))
}

// frame renders one frame: header, zeroed side info, then payload padded out to
// the frame size the header declares.
func frame(h frameheader.FrameHeader, payload []byte) []byte {
	size, err := h.FrameSize()
	if err != nil {
		panic(err)
	}
	if got := 4 + h.SideInfoSize() + len(payload); got > size {
		panic(fmt.Sprintf("testaudio: payload of %d bytes overflows a %d-byte frame", got, size))
	}
	buf := make([]byte, size)
	binary.BigEndian.PutUint32(buf, uint32(h))
	copy(buf[4+h.SideInfoSize():], payload)
	return buf
}

// xingFrame renders the VBR header frame that precedes the audio frames.
func (o Options) xingFrame(version consts.Version, bitrateKbps int, byteCount uint32) []byte {
	tag := "Xing"
	if o.XingMode == Info {
		tag = "Info"
	}

	frameCount := o.XingFrameCount
	if frameCount == 0 {
		frameCount = uint32(o.Frames) //nolint:gosec // synthesised files hold hundreds of frames
	}

	flags := uint32(lameinfo.FlagFrameCount | lameinfo.FlagTOC | lameinfo.FlagVBRScale)
	if !o.OmitByteCount {
		flags |= uint32(lameinfo.FlagByteCount)
	}

	payload := append([]byte(tag), be32(flags)...)
	payload = append(payload, be32(frameCount)...)
	if !o.OmitByteCount {
		payload = append(payload, be32(byteCount)...)
	}
	toc := make([]byte, 100)
	for i := range toc {
		toc[i] = byte(i * 255 / 99)
	}
	payload = append(payload, toc...)
	payload = append(payload, be32(0)...) // VBR scale

	if o.LAME {
		lame := make([]byte, 9+12+3+12)
		copy(lame, "LAME3.100")
		lame[9+12] = byte(o.EncoderDelay >> 4)
		lame[9+12+1] = byte(o.EncoderDelay<<4) | byte(o.EncoderPadding>>8)
		lame[9+12+2] = byte(o.EncoderPadding)
		payload = append(payload, lame...)
	}

	return frame(header(version, o.Mono, o.SampleRateIndex, bitrateKbps, false), payload)
}

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// id3v2Tag renders an ID3v2 tag with a zero-filled payload of the given size.
func id3v2Tag(size int) []byte {
	tag := make([]byte, 10+size)
	copy(tag, "ID3")
	tag[3], tag[4] = 3, 0 // version 2.3.0
	//nolint:gosec // sizes in tests are far below the 28-bit syncsafe limit
	s := uint32(size)
	tag[6] = byte((s >> 21) & 0x7f)
	tag[7] = byte((s >> 14) & 0x7f)
	tag[8] = byte((s >> 7) & 0x7f)
	tag[9] = byte(s & 0x7f)
	return tag
}
