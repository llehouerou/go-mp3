package testaudio

import (
	"bytes"
	"io"
)

// Counter wraps a byte slice as an io.ReadSeeker that records the I/O the
// decoder performs on it. The whole point of the seek-index work is the shape
// of this traffic, so tests assert on it directly.
type Counter struct {
	r *bytes.Reader

	Reads     int
	Seeks     int
	BytesRead int
	// MaxOffset is the highest byte offset ever read, so a test can tell a
	// header-only peek from a full walk of the file.
	MaxOffset int64
}

// NewCounter wraps data. It reports itself as an io.Seeker.
func NewCounter(data []byte) *Counter {
	return &Counter{r: bytes.NewReader(data)}
}

// NewUnseekableCounter wraps data as a plain io.Reader with no Seek method, so
// it stands in for an HTTP body or a pipe.
func NewUnseekableCounter(data []byte) *UnseekableCounter {
	return &UnseekableCounter{Stats: NewCounter(data)}
}

func (c *Counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.Reads++
	c.BytesRead += n
	if off := c.offset(); off > c.MaxOffset {
		c.MaxOffset = off
	}
	return n, err
}

func (c *Counter) Seek(offset int64, whence int) (int64, error) {
	c.Seeks++
	return c.r.Seek(offset, whence)
}

func (c *Counter) offset() int64 {
	off, err := c.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0
	}
	return off
}

// UnseekableCounter exposes only Read, so a type assertion to io.Seeker fails.
// Its counters live in Stats.
type UnseekableCounter struct {
	Stats *Counter
}

// Read implements io.Reader.
func (u *UnseekableCounter) Read(p []byte) (int, error) { return u.Stats.Read(p) }
