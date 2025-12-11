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

If sample-accurate playback is critical for your use case, you may need to detect and skip the initial silence yourself, or use a format like FLAC or WAV that doesn't have this issue.
