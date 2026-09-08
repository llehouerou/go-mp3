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
	"bufio"
	"errors"
	"io"

	"github.com/llehouerou/go-mp3/internal/frameheader"
)

// sourceBufferSize is the block size reads are batched into. MP3 frames are a
// few hundred bytes, so unbuffered decoding costs one syscall per frame, which
// on a network filesystem is one round trip per frame.
//
// ponytail: one fixed size for every source; make it an option only if a caller
// turns up that genuinely needs a different one.
const sourceBufferSize = 64 * 1024

type source struct {
	// reader is the original reader, kept for its io.Seeker identity.
	reader io.Reader
	// br batches reads from reader.
	br *bufio.Reader
	// buf holds bytes pushed back by Unread, consumed before br.
	buf []byte
	pos int64
}

func newSource(r io.Reader) *source {
	return &source{
		reader: r,
		br:     bufio.NewReaderSize(r, sourceBufferSize),
	}
}

// Seek moves the logical read position.
//
// Buffering makes the underlying reader's position meaningless to callers: it
// sits wherever the last block fetch left it, ahead of the byte the decoder
// will read next. Every seek is therefore resolved to an absolute target first,
// and relative seeks are never passed through.
func (s *source) Seek(position int64, whence int) (int64, error) {
	seeker, ok := s.reader.(io.Seeker)
	if !ok {
		return 0, errors.New("mp3: source must be io.Seeker")
	}

	target := position
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		target = s.pos + position
	default: // io.SeekEnd: only the underlying reader knows where the end is,
		// and the seek it performs lands us there, so there is nothing left to do.
		n, err := seeker.Seek(position, whence)
		if err != nil {
			return 0, err
		}
		s.buf = nil
		s.br.Reset(s.reader)
		s.pos = n
		return n, nil
	}

	// A short forward seek is what walking frame headers does: read four
	// bytes, skip the body. Serving it from the buffer is the whole point of
	// buffering; seeking the underlying reader would throw the block away and
	// refetch it for the next header.
	if delta := target - s.pos; len(s.buf) == 0 && delta >= 0 && delta <= int64(s.br.Buffered()) {
		if _, err := s.br.Discard(int(delta)); err != nil {
			return 0, err
		}
		s.pos = target
		return s.pos, nil
	}

	s.buf = nil
	n, err := seeker.Seek(target, io.SeekStart)
	if err != nil {
		return 0, err
	}
	s.br.Reset(s.reader)
	s.pos = n
	return n, nil
}

// Peek returns the next n bytes without consuming them, so metadata in the
// first frame can be read on any source, seekable or not. Fewer bytes come back
// near the end of the stream.
func (s *source) Peek(n int) ([]byte, error) {
	if len(s.buf) >= n {
		return s.buf[:n], nil
	}
	rest, err := s.br.Peek(n - len(s.buf))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(s.buf) == 0 {
		return rest, nil
	}
	return append(append([]byte{}, s.buf...), rest...), nil
}

// nextFrame finds the next frame header and reports where it starts, leaving
// the source positioned just after it. The end of the audio is io.EOF, whatever
// shape it takes underneath: the source running out, a header cut short, or a
// stretch of trailing tags or garbage longer than the sync search tolerates.
func (s *source) nextFrame() (h frameheader.FrameHeader, start int64, err error) {
	h, start, err = frameheader.Read(s, s.pos)
	return h, start, endOfAudio(err)
}

// endOfAudio folds every error that means "no more audio" into io.EOF and
// leaves any other error alone. The nil check is not just a shortcut: the
// errors.As targets escape to the heap, and this runs once per frame.
func endOfAudio(err error) error {
	if err == nil {
		return nil
	}
	var truncated *frameheader.UnexpectedEOFError
	var noSync *frameheader.SyncSearchLimitError
	if errors.As(err, &truncated) || errors.As(err, &noSync) {
		return io.EOF
	}
	return err
}

func (s *source) skipTags() error {
	for {
		buf := make([]byte, 3)
		if _, err := s.ReadFull(buf); err != nil {
			return err
		}
		switch string(buf) {
		case "TAG":
			buf := make([]byte, 125)
			if _, err := s.ReadFull(buf); err != nil {
				return err
			}

		case "ID3":
			// Skip version (2 bytes) and flag (1 byte)
			buf := make([]byte, 3)
			if _, err := s.ReadFull(buf); err != nil {
				return err
			}

			buf = make([]byte, 4)
			n, err := s.ReadFull(buf)
			if err != nil {
				return err
			}
			if n != 4 {
				return nil
			}
			//nolint:gosec // buf is guaranteed to have 4 elements after ReadFull check above
			size := (uint32(buf[0]) << 21) | (uint32(buf[1]) << 14) |
				(uint32(buf[2]) << 7) | uint32(buf[3])
			buf = make([]byte, size)
			if _, err := s.ReadFull(buf); err != nil {
				return err
			}

		default:
			s.Unread(buf)
			return nil
		}
	}
}

func (s *source) rewind() error {
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		return err
	}
	s.pos = 0
	s.buf = nil
	return nil
}

// Unread pushes bytes back so the next read returns them again, rewinding the
// logical position by the same amount.
func (s *source) Unread(buf []byte) {
	s.buf = append(buf, s.buf...)
	s.pos -= int64(len(buf))
}

// takePushback consumes up to len(buf) pushed-back bytes, advancing the logical
// position by what it hands over.
func (s *source) takePushback(buf []byte) int {
	if len(s.buf) == 0 {
		return 0
	}
	n := copy(buf, s.buf)
	if len(s.buf) > n {
		s.buf = s.buf[n:]
	} else {
		s.buf = nil
	}
	s.pos += int64(n)
	return n
}

func (s *source) ReadFull(buf []byte) (int, error) {
	read := s.takePushback(buf)
	if read == len(buf) {
		return read, nil
	}

	n, err := io.ReadFull(s.br, buf[read:])
	if err != nil {
		// Allow if all data can't be read. This is common.
		if err == io.ErrUnexpectedEOF {
			err = io.EOF
		}
	}
	s.pos += int64(n)
	return n + read, err
}
