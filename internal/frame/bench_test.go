package frame

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/llehouerou/go-mp3/internal/frameheader"
)

type fullReader struct{ *bytes.Reader }

func (r fullReader) ReadFull(buf []byte) (int, error) { return io.ReadFull(r.Reader, buf) }

// benchFrame returns a frame from well inside the music of classic_lame.mp3,
// with the synthesis history of the frames before it already in place.
func benchFrame(b *testing.B) *Frame {
	b.Helper()
	buf, err := os.ReadFile("../../example/classic_lame.mp3")
	if err != nil {
		b.Fatal(err)
	}
	src := fullReader{bytes.NewReader(buf)}
	f := &Frame{}
	read := func() {
		h, _, err := frameheader.Read(src, 0)
		if err != nil {
			b.Fatal(err)
		}
		if err := f.Read(src, h); err != nil {
			b.Fatal(err)
		}
	}
	var out []byte
	for range 40 {
		read()
		out = make([]byte, f.BytesPerFrame())
		f.Decode(out)
	}
	read()
	return f
}

// BenchmarkFrame times one frame through the whole pipeline and through each
// stage in isolation. Stages that overwrite mainData.Is get it restored before
// every iteration; the synthesis history is left to evolve, as it does in a
// real stream.
func BenchmarkFrame(b *testing.B) {
	f := benchFrame(b)
	nch := f.header.NumberOfChannels()
	g := &f.g.Ch[0]
	all := f.g.Ch
	out := make([]byte, f.BytesPerFrame())

	b.Run("Decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f.g.Ch = all
			f.Decode(out)
		}
	})

	saved := *g
	b.Run("requantize", func(b *testing.B) {
		for b.Loop() {
			*g = saved
			for ch := range nch {
				f.requantize(&g[ch])
			}
		}
	})

	for ch := range nch {
		f.requantize(&g[ch])
		f.reorder(&g[ch])
	}
	f.stereo(g)
	for ch := range nch {
		antialias(&g[ch])
	}
	saved = *g
	store := f.store

	b.Run("hybridSynthesis", func(b *testing.B) {
		for b.Loop() {
			*g = saved
			f.store = store
			for ch := range nch {
				f.hybridSynthesis(&g[ch], ch)
			}
		}
	})

	for ch := range nch {
		f.hybridSynthesis(&g[ch], ch)
	}

	b.Run("subbandSynthesis", func(b *testing.B) {
		for b.Loop() {
			for ch := range nch {
				f.subbandSynthesis(&g[ch], ch, out)
			}
		}
	})
}
