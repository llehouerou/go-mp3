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
	source        *source
	sampleRate    int
	length        int64 // Virtual (trimmed) length, or -1
	rawLength     int64 // Raw decoded bytes before trimming, or -1
	frameStarts   []int64
	buf           []byte
	frame         *frame.Frame
	pos           int64 // Virtual position (after trimming)
	bytesPerFrame int64

	// Gapless playback
	lameInfo       *lameinfo.Info
	skipStartBytes int64 // Bytes to skip at start (Xing frame + delay)
	skipEndBytes   int64 // Bytes to trim at end (padding)
}

func (d *Decoder) readFrame() error {
	var err error
	d.frame, _, err = frame.Read(d.source, d.source.pos, d.frame)
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
	d.buf = append(d.buf, d.frame.Decode()...)
	return nil
}

// Read is io.Reader's Read.
func (d *Decoder) Read(buf []byte) (int, error) {
	for len(d.buf) == 0 {
		// Check if we've reached the virtual end (gapless mode)
		if d.lameInfo != nil && d.length != invalidLength && d.pos >= d.length {
			return 0, io.EOF
		}

		if err := d.readFrame(); err != nil {
			return 0, err
		}

		// If gapless, trim buffer if it would exceed the virtual end
		if d.lameInfo != nil && d.length != invalidLength {
			remaining := d.length - d.pos
			if remaining <= 0 {
				d.buf = nil
				return 0, io.EOF
			}
			if int64(len(d.buf)) > remaining {
				d.buf = d.buf[:remaining]
			}
		}
	}
	n := copy(buf, d.buf)
	d.buf = d.buf[n:]
	d.pos += int64(n)
	return n, nil
}

// Seek is io.Seeker's Seek.
//
// Seek returns an error when the underlying source is not io.Seeker.
//
// Note that seek uses a byte offset but samples are aligned to 4 bytes (2
// channels, 2 bytes each). Be careful to seek to an offset that is divisible by
// 4 if you want to read at full sample boundaries.
func (d *Decoder) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekCurrent {
		// Handle the special case of asking for the current position specially.
		return d.pos, nil
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
	d.frame = nil

	// Handle seeking to end of file - no frames to read
	if d.length != invalidLength && d.pos >= d.length {
		return npos, nil
	}

	// Convert virtual position to raw position for frame lookup
	rawPos := npos
	if d.lameInfo != nil {
		rawPos = npos + d.skipStartBytes
	}

	f := rawPos / d.bytesPerFrame
	// If the frame is not first, read the previous ahead of reading that
	// because the previous frame can affect the targeted frame.
	if f > 0 {
		f--
		if _, err := d.source.Seek(d.frameStarts[f], 0); err != nil {
			return 0, err
		}
		if err := d.readFrame(); err != nil {
			return 0, err
		}
		if err := d.readFrame(); err != nil {
			return 0, err
		}
		d.buf = d.buf[d.bytesPerFrame+(rawPos%d.bytesPerFrame):]
	} else {
		// Seeking near start - need to handle gapless skip
		if _, err := d.source.Seek(d.frameStarts[0], 0); err != nil {
			return 0, err
		}

		if d.lameInfo != nil {
			// Re-apply gapless start skipping, then seek within remaining buffer
			d.buf = nil
			d.skipInitialSamples()
			// Now skip to the requested virtual position within the buffer
			if npos > 0 {
				for int64(len(d.buf)) < npos {
					if err := d.readFrame(); err != nil {
						return 0, err
					}
				}
				if npos <= int64(len(d.buf)) {
					d.buf = d.buf[npos:]
				}
			}
		} else {
			if err := d.readFrame(); err != nil {
				return 0, err
			}
			d.buf = d.buf[npos:]
		}
	}
	return npos, nil
}

// SampleRate returns the sample rate like 44100.
//
// Note that the sample rate is retrieved from the first frame.
func (d *Decoder) SampleRate() int {
	return d.sampleRate
}

func (d *Decoder) ensureFrameStartsAndLength() error {
	if d.length != invalidLength {
		return nil
	}

	if _, ok := d.source.reader.(io.Seeker); !ok {
		return nil
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
		d.bytesPerFrame = int64(h.BytesPerFrame())
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
	d.length = l

	if _, err := d.source.Seek(pos, io.SeekStart); err != nil {
		return err
	}
	return nil
}

const invalidLength = -1

// Length returns the total size in bytes.
//
// Length returns -1 when the total size is not available
// e.g. when the given source is not io.Seeker.
func (d *Decoder) Length() int64 {
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
// Returns -1 if the source is not seekable.
// If gapless mode is not active, this returns the same value as Length().
func (d *Decoder) RawLength() int64 {
	if d.rawLength != invalidLength {
		return d.rawLength
	}
	return d.length
}

// Duration returns the total duration of the audio stream.
// Returns -1 if the duration cannot be determined (e.g., non-seekable source).
func (d *Decoder) Duration() time.Duration {
	if d.length == invalidLength {
		return -1
	}
	return d.bytesToDuration(d.length)
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
	if d.length == invalidLength {
		return -1
	}
	if d.length == 0 {
		return 0
	}
	return float64(d.pos) / float64(d.length)
}

// SamplePosition returns the current position in samples (per channel).
// Each sample is 4 bytes (stereo 16-bit).
func (d *Decoder) SamplePosition() int64 {
	return d.pos / 4
}

// SampleCount returns the total number of samples (per channel).
// Returns -1 if the count cannot be determined.
func (d *Decoder) SampleCount() int64 {
	if d.length == invalidLength {
		return -1
	}
	return d.length / 4
}

// SeekToSample seeks to the specified sample position.
// Returns an error if seeking is not supported.
// Negative positions are clamped to 0, positions beyond the end are clamped.
func (d *Decoder) SeekToSample(sample int64) error {
	// Check if seeking is supported
	if d.length == invalidLength {
		return errors.New("mp3: seek not supported on non-seekable source")
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
	// Check if seeking is supported
	if d.length == invalidLength {
		return errors.New("mp3: seek not supported on non-seekable source")
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
	s := &source{
		reader: r,
	}
	d := &Decoder{
		source:    s,
		length:    invalidLength,
		rawLength: invalidLength,
	}

	if err := s.skipTags(); err != nil {
		return nil, err
	}

	// Save position before first frame for LAME parsing
	firstFramePos := s.pos

	if err := d.readFrame(); err != nil {
		return nil, err
	}
	freq, err := d.frame.SamplingFrequency()
	if err != nil {
		return nil, err
	}
	d.sampleRate = freq

	if err := d.ensureFrameStartsAndLength(); err != nil {
		return nil, err
	}

	// Try to parse gapless info and apply trimming
	if opts.Gapless {
		d.tryApplyGapless(firstFramePos)
	}

	return d, nil
}

// tryApplyGapless attempts to parse LAME/Xing metadata and configure gapless playback.
// It requires a seekable source. If parsing fails, the decoder falls back to raw mode.
func (d *Decoder) tryApplyGapless(firstFramePos int64) {
	// Need seekable source
	seeker, ok := d.source.reader.(io.Seeker)
	if !ok {
		return
	}

	// Save current position
	curPos, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}

	// Seek to first frame
	if _, err := seeker.Seek(firstFramePos, io.SeekStart); err != nil {
		return
	}
	d.source.pos = firstFramePos
	d.source.buf = nil

	// Parse LAME info
	info, err := lameinfo.ParseFromReader(d.source)
	if err != nil {
		// No LAME info - restore and use raw mode
		_, _ = seeker.Seek(curPos, io.SeekStart)
		d.source.pos = curPos
		d.source.buf = nil
		return
	}

	d.lameInfo = info

	// Calculate skip amounts
	// Skip: Xing frame (bytesPerFrame) + TotalDelay() samples
	xingFrameBytes := d.bytesPerFrame
	delayBytes := int64(info.TotalDelay()) * 4
	d.skipStartBytes = xingFrameBytes + delayBytes
	d.skipEndBytes = int64(info.TotalPadding()) * 4

	// Store raw length, calculate virtual length
	d.rawLength = d.length
	d.length = max(0, d.rawLength-d.skipStartBytes-d.skipEndBytes)

	// Restore source position for future reads
	_, _ = seeker.Seek(curPos, io.SeekStart)
	d.source.pos = curPos
	d.source.buf = nil

	// Clear the Xing frame samples from buffer and skip initial samples
	d.buf = nil
	d.pos = 0
	d.skipInitialSamples()
}

// skipInitialSamples reads and discards samples at the start for gapless playback.
// Note: This only skips the delay portion. The Xing frame is already discarded
// by setting d.buf = nil before calling this function.
func (d *Decoder) skipInitialSamples() {
	if d.lameInfo == nil {
		return
	}

	// Only skip the delay bytes - the Xing frame was already discarded
	delayBytes := int64(d.lameInfo.TotalDelay()) * 4
	if delayBytes <= 0 {
		return
	}

	remaining := delayBytes

	for remaining > 0 {
		if len(d.buf) == 0 {
			if err := d.readFrame(); err != nil {
				return
			}
		}

		if int64(len(d.buf)) <= remaining {
			remaining -= int64(len(d.buf))
			d.buf = nil
		} else {
			d.buf = d.buf[remaining:]
			remaining = 0
		}
	}
}
