//go:build ignore

// gen lifts the first audio frames out of example/classic_lame.mp3 into
// testdata/frames.bin, header and body verbatim, so synthesised files can carry
// real Huffman data through the whole decoder. Run from the repo root:
//
//	go run ./internal/testaudio/gen.go
package main

import (
	"bytes"
	"io"
	"log"
	"os"

	"github.com/llehouerou/go-mp3/internal/frameheader"
)

const (
	src    = "example/classic_lame.mp3"
	dst    = "internal/testaudio/testdata/frames.bin"
	frames = 64
)

type fullReader struct{ *bytes.Reader }

func (r fullReader) ReadFull(p []byte) (int, error) { return io.ReadFull(r.Reader, p) }

func main() {
	data, err := os.ReadFile(src)
	if err != nil {
		log.Fatal(err)
	}
	if bytes.HasPrefix(data, []byte("ID3")) {
		size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		data = data[10+size:]
	}

	r := fullReader{bytes.NewReader(data)}
	var out []byte
	for taken := 0; taken < frames; {
		h, _, err := frameheader.Read(r, 0)
		if err != nil {
			log.Fatal(err)
		}
		size, err := h.FrameSize()
		if err != nil {
			log.Fatal(err)
		}
		frame := make([]byte, size)
		if _, err := r.ReadFull(frame[4:]); err != nil {
			log.Fatal(err)
		}
		tag := frame[4+h.SideInfoSize():]
		if bytes.HasPrefix(tag, []byte("Xing")) || bytes.HasPrefix(tag, []byte("Info")) {
			continue
		}
		frame[0], frame[1], frame[2], frame[3] = byte(h>>24), byte(h>>16), byte(h>>8), byte(h)
		out = append(out, frame...)
		taken++
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d frames, %d bytes", frames, len(out))
}
