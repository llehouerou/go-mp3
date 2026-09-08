// Copyright 2017 Hajime Hoshi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mp3

import (
	"errors"
	"io"
	"time"

	"github.com/llehouerou/go-mp3/internal/consts"
	"github.com/llehouerou/go-mp3/internal/frame"
	"github.com/llehouerou/go-mp3/internal/frameheader"
	"github.com/llehouerou/go-mp3/lameinfo"
)

// DecoderOptions configures the MP3 decoder behavior.
type DecoderOptions struct {
	// Gapless enables gapless playback by trimming encoder delay and padding.
	// When true (default), the decoder reads LAME/Xing metadata and adjusts
	// the audio stream to remove encoder-added silence.
	Gapless bool
}

// DefaultDecoderOptions returns options with gapless enabled.
func DefaultDecoderOptions() DecoderOptions {
	return DecoderOptions{Gapless: true}
}

// A Decoder is a MP3-decoded stream.
//
// Decoder decodes its underlying source on the fly.
//
// A Decoder is not safe for concurrent use. If multiple goroutines need to
// access the same Decoder (e.g., one for playback and one for seeking),
// the caller must synchronize access with a mutex or similar mechanism.
type Decoder struct {
	source      *source
	sampleRate  int
	length      int64 // Virtual (trimmed) length, or -1
	rawLength   int64 // Raw decoded bytes before trimming, or -1
	frameStarts []int64
	// buf is the decoded PCM not yet handed to the caller; pcm is the array it
	// lives in, so readFrame can start over at the front once buf is drained.
	buf           []byte
	pcm           []byte
	frame         frame.Frame
	pos           int64 // Virtual position (after trimming)
	bytesPerFrame int64

	// seekable reports whether the source supports seeking. It is independent
	// of whether the length is known: a Xing header gives a length even on a
	// stream that cannot seek.
	seekable bool
	// scanned reports whether frameStarts holds the whole file. The scan is
	// only run when something actually needs it.
	scanned bool
	// firstFramePos is the offset of the first frame, after any leading tags.
	firstFramePos int64
	// fileSize is the size of a seekable source, or 0 when it is unknown.
	fileSize int64

	// lameInfo is the LAME/Xing metadata handed out by GaplessInfo; trim is what
	// the decoder actually applies, zero when gapless is off or there is no header.
	lameInfo *lameinfo.Info
	trim     trim
}

// ErrNotSeekable is returned by the seeking methods when the source is a plain
// io.Reader. Length and Duration can still be available in that case, when the
// file carries a Xing/Info header.
var ErrNotSeekable = errors.New("mp3: source is not seekable")

func (d *Decoder) readFrame() error {
	_, err := d.frame.Read(d.source, d.source.pos)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		var unexpectedEOF *consts.UnexpectedEOFError
		if errors.As(err, &unexpectedEOF) {
			// TODO: Log here?
			return io.EOF
		}
		// If we can't find a valid frame header, we've likely hit
		// trailing metadata (APE tags, ID3v1, etc.) - treat as end of audio
		var syncLimitErr *frameheader.SyncSearchLimitError
		if errors.As(err, &syncLimitErr) {
			return io.EOF
		}
		return err
	}
	if len(d.buf) == 0 {
		d.buf = d.pcm[:0]
	}
	n := len(d.buf)
	d.buf = append(d.buf, make([]byte, d.frame.BytesPerFrame())...)
	d.frame.Decode(d.buf[n:])
	if n == 0 {
		d.pcm = d.buf
	}
	return nil
}

// Read is io.Reader's Read.
func (d *Decoder) Read(buf []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.length != invalidLength && d.pos >= d.length {
			return 0, io.EOF
		}
		if err := d.readFrame(); err != nil {
			return 0, err
		}
		// Never hand out bytes past the end of the audio.
		if remaining := d.length - d.pos; d.length != invalidLength && int64(len(d.buf)) > remaining {
			d.buf = d.buf[:remaining]
		}
	}
	n := copy(buf, d.buf)
	d.buf = d.buf[n:]
	d.pos += int64(n)
	return n, nil
}

// Seek is io.Seeker's Seek.
//
// Seek returns ErrNotSeekable when the underlying source is not io.Seeker. The
// first seek on a file without a Xing header walks its frame headers to build
// an index; later seeks reuse it.
//
// Note that seek uses a byte offset but samples are aligned to 4 bytes (2
// channels, 2 bytes each). Be careful to seek to an offset that is divisible by
// 4 if you want to read at full sample boundaries.
func (d *Decoder) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekCurrent {
		// Handle the special case of asking for the current position specially.
		return d.pos, nil
	}

	if err := d.ensureFrameIndex(); err != nil {
		return 0, err
	}

	npos := int64(0)
	switch whence {
	case io.SeekStart:
		npos = offset
	case io.SeekCurrent:
		npos = d.pos + offset
	case io.SeekEnd:
		npos = d.Length() + offset
	default:
		return 0, errors.New("mp3: invalid whence")
	}

	// Clamp to valid range
	if npos < 0 {
		npos = 0
	}
	if d.length != invalidLength && npos > d.length {
		npos = d.length
	}

	d.pos = npos
	d.buf = nil
	d.frame.Reset()

	// Handle seeking to end of file - no frames to read
	if d.length != invalidLength && d.pos >= d.length {
		return npos, nil
	}

	if len(d.frameStarts) == 0 {
		return 0, errors.New("mp3: no frames to seek to")
	}
	// Start one frame early so the bit reservoir the target frame points back
	// into is there. The frame index is clamped because a Xing header can
	// promise more frames than the file holds.
	rawPos := d.trim.rawOf(npos)
	start := max(min(rawPos/d.bytesPerFrame, int64(len(d.frameStarts)-1))-1, 0)
	if _, err := d.source.Seek(d.frameStarts[start], io.SeekStart); err != nil {
		return 0, err
	}
	if err := d.advance(rawPos - start*d.bytesPerFrame); err != nil {
		return 0, err
	}
	return npos, nil
}

// advance decodes forward until the buffer covers lead raw bytes and drops
// them. Running into EOF first leaves the buffer empty: the target lies past
// the audio the file actually holds.
func (d *Decoder) advance(lead int64) error {
	for int64(len(d.buf)) < lead {
		if err := d.readFrame(); err != nil {
			if errors.Is(err, io.EOF) {
				d.buf = nil
				return nil
			}
			return err
		}
	}
	d.buf = d.buf[lead:]
	return nil
}

// SampleRate returns the sample rate like 44100.
//
// Note that the sample rate is retrieved from the first frame.
func (d *Decoder) SampleRate() int {
	return d.sampleRate
}

// ensureLength makes the total length known, scanning the file if that is the
// only way to learn it.
func (d *Decoder) ensureLength() error {
	if d.rawLength != invalidLength {
		return nil
	}
	return d.scan()
}

// ensureFrameIndex makes frameStarts cover the whole file. Playback never needs
// it; only seeking does, so it is built on the first seek and kept.
func (d *Decoder) ensureFrameIndex() error {
	if d.scanned {
		return nil
	}
	return d.scan()
}

// scan walks every frame header in the file, building the frame index and, when
// it is not already known from a Xing header, the length.
//
// This is what opening a file used to do unconditionally. It reads the whole
// file, so it now runs only when something asks for what it produces.
func (d *Decoder) scan() error {
	if !d.seekable {
		return ErrNotSeekable
	}

	// Keep the current position.
	pos, err := d.source.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if err := d.source.rewind(); err != nil {
		return err
	}

	if err := d.source.skipTags(); err != nil {
		return err
	}
	l := int64(0)
	for {
		h, pos, err := frameheader.Read(d.source, d.source.pos)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			var unexpectedEOF *consts.UnexpectedEOFError
			if errors.As(err, &unexpectedEOF) {
				// TODO: Log here?
				break
			}
			// If we can't find a valid frame header, we've likely hit
			// trailing metadata (APE tags, ID3v1, etc.) - treat as end of audio
			var syncLimitErr *frameheader.SyncSearchLimitError
			if errors.As(err, &syncLimitErr) {
				break
			}
			return err
		}
		d.frameStarts = append(d.frameStarts, pos)
		l += d.bytesPerFrame

		framesize, err := h.FrameSize()
		if err != nil {
			return err
		}
		if _, err := d.source.Seek(int64(framesize-4), io.SeekCurrent); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	d.scanned = true
	// A length derived from a Xing header wins: it is what the file claims
	// about itself, it was already sanity-checked, and letting a later seek
	// change Duration() under the caller would be worse than a small
	// disagreement.
	if d.rawLength == invalidLength {
		d.setRawLength(l)
	}

	if _, err := d.source.Seek(pos, io.SeekStart); err != nil {
		return err
	}
	return nil
}

const invalidLength = -1

// setRawLength records the raw decoded size and derives the length callers see
// from it, which is the raw size minus what gapless trimming removes.
func (d *Decoder) setRawLength(raw int64) {
	d.rawLength = raw
	d.length = d.trim.virtualLength(raw)
}

// Length returns the total size in bytes.
//
// It is free when the file carries a Xing/Info header. Without one, the first
// call walks the file's frame headers to count them.
//
// Length returns -1 when the total size cannot be determined, i.e. when the
// source is neither seekable nor carrying a Xing/Info header.
func (d *Decoder) Length() int64 {
	if err := d.ensureLength(); err != nil {
		return invalidLength
	}
	return d.length
}

// BytesPerFrame returns the number of decoded bytes per MP3 frame.
// This is useful for calculating frame timing or positions.
func (d *Decoder) BytesPerFrame() int64 {
	return d.bytesPerFrame
}

// GaplessInfo returns the LAME/Xing gapless metadata if available.
// Returns nil if no gapless metadata was found or gapless mode is disabled.
func (d *Decoder) GaplessInfo() *lameinfo.Info {
	return d.lameInfo
}

// RawLength returns the total decoded bytes without gapless trimming.
// Returns -1 if the length cannot be determined.
// If gapless mode is not active, this returns the same value as Length().
func (d *Decoder) RawLength() int64 {
	if err := d.ensureLength(); err != nil {
		return invalidLength
	}
	return d.rawLength
}

// Duration returns the total duration of the audio stream.
// Returns -1 if the duration cannot be determined.
func (d *Decoder) Duration() time.Duration {
	length := d.Length()
	if length == invalidLength {
		return -1
	}
	return d.bytesToDuration(length)
}

// Position returns the current playback position as a time.Duration.
func (d *Decoder) Position() time.Duration {
	return d.bytesToDuration(d.pos)
}

// Remaining returns the remaining duration from the current position.
// Returns -1 if duration cannot be determined.
func (d *Decoder) Remaining() time.Duration {
	dur := d.Duration()
	if dur < 0 {
		return -1
	}
	return dur - d.Position()
}

// Progress returns the playback progress as a value between 0.0 and 1.0.
// Returns -1 if progress cannot be determined.
func (d *Decoder) Progress() float64 {
	length := d.Length()
	if length == invalidLength {
		return -1
	}
	if length == 0 {
		return 0
	}
	return float64(d.pos) / float64(length)
}

// SamplePosition returns the current position in samples (per channel).
// Each sample is 4 bytes (stereo 16-bit).
func (d *Decoder) SamplePosition() int64 {
	return d.pos / 4
}

// SampleCount returns the total number of samples (per channel).
// Returns -1 if the count cannot be determined.
func (d *Decoder) SampleCount() int64 {
	length := d.Length()
	if length == invalidLength {
		return -1
	}
	return length / 4
}

// SeekToSample seeks to the specified sample position.
// Returns an error if seeking is not supported.
// Negative positions are clamped to 0, positions beyond the end are clamped.
func (d *Decoder) SeekToSample(sample int64) error {
	if !d.seekable {
		return ErrNotSeekable
	}

	// Clamp to valid range
	if sample < 0 {
		sample = 0
	}
	maxSamples := d.SampleCount()
	if sample > maxSamples {
		sample = maxSamples
	}

	// Convert to bytes (4 bytes per sample)
	bytes := sample * 4
	_, err := d.Seek(bytes, io.SeekStart)
	return err
}

// Skip seeks relative to the current position by the specified duration.
// Positive values skip forward, negative values skip backward.
// Returns an error if seeking is not supported.
// The result is clamped to the valid range [0, Duration].
func (d *Decoder) Skip(delta time.Duration) error {
	return d.SeekToTime(d.Position() + delta)
}

// SeekToTime seeks to the specified absolute time position.
// Returns an error if seeking is not supported.
// Negative times are clamped to 0, times beyond duration are clamped to the end.
func (d *Decoder) SeekToTime(t time.Duration) error {
	if !d.seekable {
		return ErrNotSeekable
	}

	// Clamp to valid range
	if t < 0 {
		t = 0
	}
	maxDur := d.Duration()
	if t > maxDur {
		t = maxDur
	}

	// Convert to bytes and align to 4-byte sample boundary
	bytes := d.durationToBytes(t)
	bytes &^= 3 // Align to 4-byte boundary

	_, err := d.Seek(bytes, io.SeekStart)
	return err
}

// bytesToDuration converts a byte position to a time.Duration.
func (d *Decoder) bytesToDuration(bytes int64) time.Duration {
	// bytes = samples * 4 (stereo 16-bit)
	// duration = samples / sampleRate = bytes / (sampleRate * 4)
	return time.Duration(int64(time.Second) * bytes / int64(d.sampleRate*4))
}

// durationToBytes converts a time.Duration to a byte position.
func (d *Decoder) durationToBytes(dur time.Duration) int64 {
	// Formula: bytes = duration_seconds * sampleRate * 4 (stereo 16-bit)
	return int64(dur) * int64(d.sampleRate*4) / int64(time.Second)
}

// NewDecoder decodes the given io.Reader and returns a decoded stream.
//
// The stream is always formatted as 16bit (little endian) 2 channels
// even if the source is single channel MP3.
// Thus, a sample always consists of 4 bytes.
//
// By default, gapless playback is enabled. Use NewDecoderWithOptions to
// configure decoder behavior.
func NewDecoder(r io.Reader) (*Decoder, error) {
	return NewDecoderWithOptions(r, DefaultDecoderOptions())
}

// NewDecoderWithOptions decodes the given io.Reader with the specified options.
//
// The stream is always formatted as 16bit (little endian) 2 channels
// even if the source is single channel MP3.
// Thus, a sample always consists of 4 bytes.
func NewDecoderWithOptions(r io.Reader, opts DecoderOptions) (*Decoder, error) {
	s := newSource(r)
	d := &Decoder{
		source:    s,
		length:    invalidLength,
		rawLength: invalidLength,
	}
	_, d.seekable = r.(io.Seeker)

	// Ask how big the file is while nothing is buffered yet, so the answer
	// costs two seeks and no reads. The credibility check below needs it, and
	// the decoder already assumes the stream starts here.
	if d.seekable {
		if size, err := s.Seek(0, io.SeekEnd); err == nil {
			d.fileSize = size
		}
		if _, err := s.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	if err := s.skipTags(); err != nil {
		return nil, err
	}
	d.firstFramePos = s.pos

	// Read the VBR header out of the first frame before decoding it. Peeking
	// costs nothing on any source, and the frame count it carries is what
	// makes counting frames by hand unnecessary.
	var info *lameinfo.Info
	if head, err := s.Peek(xingHeaderPeek); err == nil {
		info, _ = lameinfo.Parse(head)
	}

	if err := d.readFrame(); err != nil {
		return nil, err
	}
	freq, err := d.frame.SamplingFrequency()
	if err != nil {
		return nil, err
	}
	d.sampleRate = freq
	d.bytesPerFrame = int64(d.frame.BytesPerFrame())

	if opts.Gapless && info != nil {
		d.lameInfo = info
		d.trim = trimFor(info, d.bytesPerFrame)
		// Discard the Xing frame's decoded samples, then the encoder delay.
		d.buf = nil
		d.pos = 0
		if err := d.advance(d.trim.start - d.bytesPerFrame); err != nil {
			return nil, err
		}
	}
	if err := d.applyXingLength(info); err != nil {
		return nil, err
	}

	return d, nil
}

// xingHeaderPeek is the most a Xing header plus a LAME tag can occupy: the
// frame header, the largest side info block, the tag itself with every optional
// field, and the LAME extension.
const xingHeaderPeek = 4 + 32 + 4 + 4 + 4 + 4 + 100 + 4 + 36

// applyXingLength derives the total length from the frame count in the VBR
// header, falling back to counting frames when there is no header or the count
// it gives is not credible.
func (d *Decoder) applyXingLength(info *lameinfo.Info) error {
	if info != nil && info.HasFrameCount() && d.frameCountIsCredible(info) {
		// LAME counts the audio frames, excluding the header frame it wrote.
		d.setRawLength((int64(info.FrameCount) + 1) * d.bytesPerFrame)
		return nil
	}

	if info != nil && d.seekable {
		// The file lied about itself, so counting is the only way to know. Do
		// it now rather than lazily: gapless trimming needs a length to trim
		// against, and the caller was told this file has one.
		return d.scan()
	}

	// No header: leave the length unknown until something asks for it.
	return nil
}

// frameCountIsCredible rejects a frame count the file is too small to hold,
// which is what a download cut short looks like. It cannot validate a count
// that is merely wrong; the file would have to be counted for that.
func (d *Decoder) frameCountIsCredible(info *lameinfo.Info) bool {
	if !d.seekable {
		return true // No way to check, and no scan available either.
	}
	if d.fileSize == 0 {
		return true
	}
	audioBytes := d.fileSize - d.firstFramePos

	if info.HasByteCount() && info.ByteCount > 0 && audioBytes < int64(info.ByteCount) {
		return false
	}

	// Every frame occupies at least the smallest legal frame at this sample
	// rate, so the file cannot be shorter than that times the frame count.
	samplesPerFrame := d.bytesPerFrame / 4
	minBitrate := int64(32000) // MPEG1 Layer III
	if samplesPerFrame < 1152 {
		minBitrate = 8000 // MPEG2/2.5 Layer III
	}
	minFrameBytes := samplesPerFrame / 8 * minBitrate / int64(d.sampleRate)
	return audioBytes >= (int64(info.FrameCount)+1)*minFrameBytes
}
