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

## Output Accuracy

Decoded PCM meets ISO/IEC 11172-4 **limited compliance** against a reference
decoder (mpg123). Output is **not** guaranteed to be identical bit-for-bit
across versions: arithmetic changes may move the last bits of a sample. Do not
golden-hash decoder output in your own tests. See
[docs/adr/0002](docs/adr/0002-accuracy-budget-over-bit-exact-output.md).
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

## Gapless Playback

MP3 encoders add silence at the start (encoder delay) and end (padding) of files. This library automatically detects LAME/Xing metadata and trims this silence for seamless playback.

**Gapless is enabled by default.** The decoder automatically:
- Parses LAME/Xing headers from the first frame
- Skips encoder delay samples at the start
- Trims padding samples at the end
- Reports the trimmed length via `Length()` and `Duration()`

```go
// Gapless playback works automatically
d, err := mp3.NewDecoder(file)
if err != nil {
    panic(err)
}

// Length() returns the trimmed (actual audio) length
fmt.Printf("Duration: %v\n", d.Duration())
fmt.Printf("Samples: %d\n", d.SampleCount())

// Access gapless metadata if needed
if info := d.GaplessInfo(); info != nil {
    fmt.Printf("Encoder: %s\n", info.LAMEVersion)
    fmt.Printf("Delay: %d samples\n", info.TotalDelay())
    fmt.Printf("Padding: %d samples\n", info.TotalPadding())
}

// Get raw (untrimmed) length for comparison
fmt.Printf("Raw length: %d bytes\n", d.RawLength())
```

### Disabling Gapless

To decode without trimming (original behavior):

```go
opts := mp3.DecoderOptions{Gapless: false}
d, err := mp3.NewDecoderWithOptions(file, opts)
```

### Supported Encoders

Gapless playback works with files encoded by:
- **LAME** (all versions)
- **ffmpeg/libavcodec** (Lavc)
- **Gogo** and other LAME-compatible encoders

Files without LAME/Xing headers are decoded normally without trimming.

### The lameinfo Package

For advanced use cases, the `lameinfo` package provides direct access to LAME/Xing header data:

```go
import "github.com/llehouerou/go-mp3/lameinfo"

info, err := lameinfo.ParseFromReader(file)
if err == nil {
    fmt.Printf("Frame count: %d\n", info.FrameCount)
    fmt.Printf("Byte count: %d\n", info.ByteCount)
    fmt.Printf("VBR scale: %d\n", info.VBRScale)
    fmt.Printf("Encoder delay: %d\n", info.EncoderDelay)
    fmt.Printf("Encoder padding: %d\n", info.EncoderPadding)
}
```

## Known Limitations

- **Not all files have LAME headers**: Files without LAME/Xing metadata are decoded normally without gapless adjustment. On such files `Length()` and `Duration()` count the frames on first use, which reads the whole file once; with a Xing/Info header they are free.
- **Seeking requires an `io.Seeker`**: `Seek`, `SeekToSample`, `SeekToTime` and `Skip` return `ErrNotSeekable` on a plain `io.Reader`. `Length()` and `Duration()` still work there when the file carries a Xing/Info header, as does gapless trimming.
