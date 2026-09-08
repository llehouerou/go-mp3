package mp3

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/llehouerou/go-mp3/internal/testaudio"
)

// TestSourceNextFrame pins the end-of-audio policy at the one seam that owns
// it: a frame header is found wherever it sits, and anything that means "no
// more audio" is reported as io.EOF, whatever shape it takes underneath: the
// source running out, a header cut short, or a stretch of trailing tags or
// garbage longer than the sync search tolerates.
func TestSourceNextFrame(t *testing.T) {
	frame := testaudio.Build(testaudio.Options{Frames: 1})
	junk := make([]byte, 5000)

	tests := []struct {
		name  string
		data  []byte
		start int64
		eof   bool
	}{
		{"frame at the start", frame, 0, false},
		{"frame after junk", append(append([]byte{}, junk...), frame...), 5000, false},
		{"empty", nil, 0, true},
		{"header cut short", frame[:3], 0, true},
		{"ID3v1 tag then nothing", append([]byte("TAG"), make([]byte, 125)...), 0, true},
		{"garbage past the sync search limit", make([]byte, 70*1024), 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, start, err := newSource(bytes.NewReader(tt.data)).nextFrame()
			if tt.eof {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("err = %v, want io.EOF", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if start != tt.start || !h.IsValid() {
				t.Errorf("start = %d valid = %v, want start %d and a valid header", start, h.IsValid(), tt.start)
			}
		})
	}
}

// TestSourcePositionTracksBytes pins the invariant the buffered seek relies on:
// pos is the offset of the next byte a caller will get, whatever mix of reads,
// pushbacks and seeks got it there. Before buffering, pushed-back bytes were
// subtracted from pos on Unread but never added back when re-read, so pos drifted
// permanently behind the file and only the frame-sync search hid it.
func TestSourcePositionTracksBytes(t *testing.T) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	s := newSource(bytes.NewReader(data))

	buf := make([]byte, 3)
	if _, err := s.ReadFull(buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if s.pos != 3 {
		t.Fatalf("pos after reading 3 bytes = %d, want 3", s.pos)
	}

	s.Unread(buf)
	if s.pos != 0 {
		t.Fatalf("pos after unreading them = %d, want 0", s.pos)
	}

	if _, err := s.ReadFull(buf); err != nil {
		t.Fatalf("ReadFull after Unread: %v", err)
	}
	if s.pos != 3 {
		t.Errorf("pos after re-reading them = %d, want 3", s.pos)
	}
	if !bytes.Equal(buf, data[0:3]) {
		t.Errorf("re-read %v, want %v", buf, data[0:3])
	}
}

// TestSourceSeekFormsAgree pins that a relative seek lands where the equivalent
// absolute seek lands. Buffering breaks this if a relative seek is passed
// through to the underlying reader, whose position runs ahead by a whole block.
func TestSourceSeekFormsAgree(t *testing.T) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}

	// Two seeks the decoder actually performs: a short skip within the
	// buffered block (a frame body) and a long jump backwards (a seek).
	for _, tc := range []struct {
		name        string
		from, delta int64
	}{
		{"short forward skip", 10, 400},
		{"backwards jump", 3000, -2500},
		{"far forward jump", 10, 3000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relative := newSource(bytes.NewReader(data))
			if _, err := relative.Seek(tc.from, io.SeekStart); err != nil {
				t.Fatalf("Seek: %v", err)
			}
			// Force a buffer fill, so the underlying reader is ahead.
			one := make([]byte, 1)
			if _, err := relative.ReadFull(one); err != nil {
				t.Fatalf("ReadFull: %v", err)
			}
			if _, err := relative.Seek(tc.delta-1, io.SeekCurrent); err != nil {
				t.Fatalf("Seek(SeekCurrent): %v", err)
			}

			absolute := newSource(bytes.NewReader(data))
			if _, err := absolute.Seek(tc.from+tc.delta, io.SeekStart); err != nil {
				t.Fatalf("Seek(SeekStart): %v", err)
			}

			if relative.pos != absolute.pos {
				t.Errorf("relative seek landed at %d, absolute at %d", relative.pos, absolute.pos)
			}

			gotRel, gotAbs := make([]byte, 8), make([]byte, 8)
			if _, err := relative.ReadFull(gotRel); err != nil {
				t.Fatalf("ReadFull: %v", err)
			}
			if _, err := absolute.ReadFull(gotAbs); err != nil {
				t.Fatalf("ReadFull: %v", err)
			}
			if !bytes.Equal(gotRel, gotAbs) {
				t.Errorf("relative seek read %v, absolute read %v", gotRel, gotAbs)
			}
			if want := data[tc.from+tc.delta:][:8]; !bytes.Equal(gotAbs, want) {
				t.Errorf("read %v at offset %d, want %v", gotAbs, tc.from+tc.delta, want)
			}
		})
	}
}
