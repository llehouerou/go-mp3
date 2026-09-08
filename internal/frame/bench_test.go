package frame

import (
	"bytes"
	"io"
	"os"
	"testing"
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
	var f *Frame
	var pos int64
	for range 40 {
		f, pos, err = Read(src, pos, f)
		if err != nil {
			b.Fatal(err)
		}
		f.Decode()
	}
	f, _, err = Read(src, pos, f)
	if err != nil {
		b.Fatal(err)
	}
	return f
}

// BenchmarkFrame times one frame through the whole pipeline and through each
// stage in isolation. Stages that overwrite mainData.Is get it restored before
// every iteration; the synthesis history is left to evolve, as it does in a
// real stream.
func BenchmarkFrame(b *testing.B) {
	f := benchFrame(b)
	nch := f.header.NumberOfChannels()
	is := f.mainData.Is

	b.Run("Decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f.mainData.Is = is
			f.Decode()
		}
	})

	b.Run("requantize", func(b *testing.B) {
		for b.Loop() {
			f.mainData.Is[0] = is[0]
			for ch := range nch {
				f.requantize(0, ch)
			}
		}
	})

	for ch := range nch {
		f.requantize(0, ch)
		f.reorder(0, ch)
	}
	f.stereo(0)
	for ch := range nch {
		f.antialias(0, ch)
	}
	is = f.mainData.Is
	store := f.store

	b.Run("hybridSynthesis", func(b *testing.B) {
		for b.Loop() {
			f.mainData.Is[0] = is[0]
			f.store = store
			for ch := range nch {
				f.hybridSynthesis(0, ch)
			}
		}
	})

	for ch := range nch {
		f.hybridSynthesis(0, ch)
		f.frequencyInversion(0, ch)
	}
	out := make([]byte, f.BytesPerFrame())

	b.Run("subbandSynthesis", func(b *testing.B) {
		for b.Loop() {
			for ch := range nch {
				f.subbandSynthesis(0, ch, out)
			}
		}
	})
}
