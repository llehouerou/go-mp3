# go-mp3

[![Go Reference](https://pkg.go.dev/badge/github.com/llehouerou/go-mp3.svg)](https://pkg.go.dev/github.com/llehouerou/go-mp3)

An MP3 decoder in pure Go based on [PDMP3](https://github.com/technosaurus/PDMP3).

## Installation

```bash
go get github.com/llehouerou/go-mp3
```

## Usage

```go
package main

import (
	"os"

	"github.com/llehouerou/go-mp3"
)

func main() {
	f, err := os.Open("audio.mp3")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	d, err := mp3.NewDecoder(f)
	if err != nil {
		panic(err)
	}

	// d implements io.Reader and io.Seeker
	// Output is always 16-bit stereo (4 bytes per sample)
	// Use d.SampleRate() to get the sample rate
}
```

## Thread Safety

The `Decoder` is **not safe for concurrent use**. If you need to access the decoder from multiple goroutines (e.g., one goroutine reading audio for playback while another handles seeking from user input), you must synchronize access yourself.

### Example: Safe Concurrent Access

```go
type SafeDecoder struct {
	mu      sync.Mutex
	decoder *mp3.Decoder
}

func NewSafeDecoder(r io.Reader) (*SafeDecoder, error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, err
	}
	return &SafeDecoder{decoder: d}, nil
}

func (s *SafeDecoder) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decoder.Read(p)
}

func (s *SafeDecoder) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decoder.Seek(offset, whence)
}

func (s *SafeDecoder) SeekToTime(t time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decoder.SeekToTime(t)
}

func (s *SafeDecoder) Position() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decoder.Position()
}
```

## Known Limitations

### Encoder Delay (Initial Silence)

MP3 encoders (especially LAME) introduce a delay at the start of the decoded audio, typically around 528-2000+ samples of silence. This is an inherent artifact of MP3 encoding, not a decoder bug.

This library decodes frames faithfully without attempting to compensate for encoder-specific delays because:

- The exact delay varies by encoder, version, and settings
- While LAME stores delay metadata in the first frame, not all encoders do
- Automatic compensation would be unreliable across different MP3 sources

If sample-accurate playback is critical for your use case, you can use the `lameinfo` package to parse LAME/Xing headers and get the exact encoder delay and padding values.

The `lameinfo` package provides:
- `EncoderDelay` / `EncoderPadding`: Raw values from the LAME tag
- `TotalDelay()`: Encoder delay + standard decoder delay (529 samples)
- `TotalPadding()`: Samples to trim from the end
- `FrameCount` / `ByteCount`: Total frames and bytes (for VBR files)
- `TOC`: Seek table for accurate VBR seeking

Note: Not all MP3 files have LAME/Xing headers. Files without these headers will return `ErrNoXingHeader`.

### Example: Gapless Playback

```go
package main

import (
    "io"
    "os"

    "github.com/llehouerou/go-mp3"
    "github.com/llehouerou/go-mp3/lameinfo"
)

// GaplessDecoder wraps mp3.Decoder to skip encoder delay and padding.
type GaplessDecoder struct {
    decoder     *mp3.Decoder
    skipStart   int64 // bytes to skip at start
    trimEnd     int64 // bytes to trim from end
    actualLen   int64 // actual audio length in bytes
    pos         int64 // current position in gapless stream
}

// NewGaplessDecoder creates a decoder that compensates for encoder delay/padding.
func NewGaplessDecoder(f *os.File) (*GaplessDecoder, error) {
    // First, try to parse LAME info from the beginning of the file
    info, lameErr := lameinfo.ParseFromReader(f)

    // Rewind file for the MP3 decoder
    if _, err := f.Seek(0, io.SeekStart); err != nil {
        return nil, err
    }

    // Create the MP3 decoder
    decoder, err := mp3.NewDecoder(f)
    if err != nil {
        return nil, err
    }

    g := &GaplessDecoder{
        decoder:   decoder,
        actualLen: decoder.Length(),
    }

    // If we have LAME info, calculate skip/trim values
    if lameErr == nil && info.HasLAMEInfo() {
        // Convert samples to bytes (4 bytes per sample: stereo 16-bit)
        g.skipStart = int64(info.TotalDelay()) * 4
        g.trimEnd = int64(info.TotalPadding()) * 4
        g.actualLen = decoder.Length() - g.skipStart - g.trimEnd
    }

    // Skip the initial delay
    if g.skipStart > 0 {
        if _, err := decoder.Seek(g.skipStart, io.SeekStart); err != nil {
            return nil, err
        }
    }

    return g, nil
}

func (g *GaplessDecoder) Read(p []byte) (int, error) {
    // Calculate how much we can read before hitting the trim point
    remaining := g.actualLen - g.pos
    if remaining <= 0 {
        return 0, io.EOF
    }

    // Limit read to remaining actual audio
    if int64(len(p)) > remaining {
        p = p[:remaining]
    }

    n, err := g.decoder.Read(p)
    g.pos += int64(n)
    return n, err
}

func (g *GaplessDecoder) Length() int64 {
    return g.actualLen
}

func (g *GaplessDecoder) SampleRate() int {
    return g.decoder.SampleRate()
}
```
