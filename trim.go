package mp3

import "github.com/llehouerou/go-mp3/lameinfo"

// trim is the gapless trimming applied to a stream: the raw PCM bytes cut from
// its head and its tail. The zero value trims nothing, which is what a file
// without a Xing header, or a decoder with gapless disabled, gets.
type trim struct {
	start, end int64
}

// trimFor derives the trim from LAME/Xing metadata: the Xing frame itself
// (it carries no audio) plus the encoder delay at the head, the padding at
// the tail. A nil info trims nothing.
func trimFor(info *lameinfo.Info, bytesPerFrame int64) trim {
	if info == nil {
		return trim{}
	}
	return trim{
		start: bytesPerFrame + int64(info.TotalDelay())*4,
		end:   int64(info.TotalPadding()) * 4,
	}
}

// virtualLength is the length callers see for a stream of raw decoded bytes.
func (t trim) virtualLength(raw int64) int64 {
	return max(0, raw-t.start-t.end)
}

// rawOf maps a virtual position to its raw position in the decoded stream.
func (t trim) rawOf(virtual int64) int64 {
	return virtual + t.start
}
