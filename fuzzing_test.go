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
	"bytes"
	"io"
	"testing"

	"github.com/llehouerou/go-mp3/internal/testaudio"
)

// FuzzDecode mutates real LAME frames and synthesised MPEG-1/2 files, which
// is what reaches the parser paths no common encoder produces: mixed blocks,
// intensity stereo, out-of-range region counts, reservoir back-pointers into
// nothing. Any input must decode to an error or to PCM, never panic.
func FuzzDecode(f *testing.F) {
	f.Add(testaudio.Build(testaudio.Options{Frames: 3, RealAudio: true}))
	f.Add(testaudio.Build(testaudio.Options{Frames: 3, RealAudio: true, XingMode: testaudio.Xing, LAME: true, EncoderDelay: 576, EncoderPadding: 1000}))
	f.Add(testaudio.Build(testaudio.Options{Frames: 2}))
	f.Add(testaudio.Build(testaudio.Options{Frames: 2, Version: 2, Mono: true}))
	f.Add(testaudio.Build(testaudio.Options{Frames: 2, Version: 2, SampleRateIndex: 1}))
	f.Add(withIntensityStereo(testaudio.Build(testaudio.Options{Frames: 3, RealAudio: true})))
	f.Fuzz(func(_ *testing.T, data []byte) {
		d, err := NewDecoder(bytes.NewReader(data))
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(d, 1<<20))
		if _, err := d.Seek(4608, io.SeekStart); err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(d, 1<<20))
		}
	})
}

// withIntensityStereo sets the intensity bit of the mode extension in every
// frame header, since no encoder in use emits intensity stereo and the fuzzer
// does not find the bit on its own.
func withIntensityStereo(data []byte) []byte {
	src := newSource(bytes.NewReader(data))
	for {
		h, start, err := src.nextFrame()
		if err != nil {
			return data
		}
		data[start+3] |= 0x10
		size, _ := h.FrameSize()
		if _, err := src.Seek(int64(size-4), io.SeekCurrent); err != nil {
			return data
		}
	}
}

func TestFuzzing(_ *testing.T) {
	inputs := []string{
		// #3
		"\xff\xfa500000000000\xff\xff0000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"0000",
		"\xff\xfb\x100004000094\xff000000" +
			"00000000000000000000" +
			"00\u007f0\xff\xee\u007f\xff\xee\u007f\xff\xff\u007f\xff\xff\xee\u007f\xff\xff0" +
			"\xff\xff00\xff\xee\u007f\xff0000\u007f00\xff00\xee0" +
			"000\xff000\xff\xff\xee\u007f0\xff0000\u007f\xff0" +
			"00\xff0",
		"\xff\xfb\x100004000094\xff000000" +
			"00000000000000000000" +
			"00\u007f0\xff\xee\u007f\xff\xee\u007f\xff\xff\u007f\xff\xff\xee\u007f\xff\xff\u007f" +
			"\xff\xff\u007f0\xff\xee\u007f\xff0000\u007f00\xff\xff\xee\xee0" +
			"0\xee\u007f\xff000\xff\xff\xee\u007f0\xff0000\u007f\xff0" +
			"0\xff\xff0",
		"\xff\xfa\x1000000000000000000" +
			"00000000000000000000" +
			"000000000000000000\xff\xff" +
			"0\u007f\xff\xff\u007f\xff\xff\u007f\xff\xff\xfc0\xff\xef\xbf0\xef\xbf00" +
			"0\xff\xee\u007f\xff\xff\u007f\xff\xff\xee\u007f\xff\xff\u007f\xff\xff\u007f\xff00" +
			"\xff\xff00",
		"\xff\xfa00000031000000000n" +
			"s0f00000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000\u007f\xff\xff000\xff\xee",
		"\xff\xfa\x1000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000\xbf0\xef\xbf00" +
			"0\xff\xee0\xff\xff\u007f\xff\xff\xee\u007f\xff\xff\u007f\xff\xff\u007f\xff00" +
			"\xff0\xee0",
		"\xff\xfa\x100000050000000000\u007f" +
			"00000000000000000000" +
			"0000000000\xee\u007f0\xff\xff\xff\xff\u007f\xff\xff" +
			"\xee\u007f\xff\xff\u007f\xff\xff\u007f\xff\xff\xfc\xee\xff\xef\xbf0\xef\xbf00" +
			"0\xff\xee\u007f\xff\xff\u007f\xff\xff\xee\u007f\xff\xff\u007f\xff\xff\u007f\xff0\t" +
			"\xff\xff\xee\xee",
		// #22
		"\xff\xfa%00000000000000000" +
			"000000000000s0000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000",
		// #23
		"\xff\xfb%S000000v000\x00\x010000" +
			"00000000000000000000" +
			"0000\xf4000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000",
		// #24
		"\xff\xfb0x000000\xf9000\x00\x030000" +
			"000000000000\xf70000000" +
			"\x900000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"00000000000000000000" +
			"0000000000000",
	}
	for _, input := range inputs {
		b := bytes.NewReader([]byte(input))
		_, _ = NewDecoder(b)
	}
}
