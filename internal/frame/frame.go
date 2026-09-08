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

package frame

import (
	"errors"
	"fmt"
	"math"

	"github.com/llehouerou/go-mp3/internal/frameheader"
	"github.com/llehouerou/go-mp3/internal/granule"
	"github.com/llehouerou/go-mp3/internal/imdct"
)

var powtab34 = make([]float64, 8207)

func init() {
	for i := range powtab34 {
		powtab34[i] = math.Pow(float64(i), 4.0/3.0)
	}
}

// A Frame is the decoder state for one stream: the frame most recently read
// and the history that carries between frames. Read replaces the former in
// place, so one Frame serves a whole stream.
type Frame struct {
	header frameheader.FrameHeader
	// g holds the frame's granules and carries the bit reservoir.
	g granule.Reader
	// store is the IMDCT overlap-add history.
	store [2][32][18]float32
	// vVec is the polyphase synthesis history, used as a circular buffer:
	// vOff[ch] is where its logical index 0 currently sits.
	vVec [2][1024]float32
	vOff [2]int
}

// Reset forgets the reservoir and the synthesis history, for decoding from
// somewhere else in the stream.
func (f *Frame) Reset() {
	*f = Frame{}
}

type FullReader interface {
	ReadFull([]byte) (int, error)
}

// Read parses the frame whose header h was just read from source into f.
func (f *Frame) Read(source FullReader, h frameheader.FrameHeader) error {
	if h.ID() == frameheader.Version2_5 {
		return errors.New("mp3: MPEG version 2.5 is not supported")
	}
	if h.Layer() != frameheader.Layer3 {
		return fmt.Errorf("mp3: only layer3 (want %d; got %d) is supported", frameheader.Layer3, h.Layer())
	}
	f.header = h
	return f.g.Read(source, h)
}

func (f *Frame) SamplingFrequency() (int, error) {
	return f.header.SamplingFrequencyValue()
}

// BytesPerFrame returns the number of decoded PCM bytes this frame yields.
func (f *Frame) BytesPerFrame() int {
	return f.header.BytesPerFrame()
}

// Decode writes the frame's PCM into out, which must hold BytesPerFrame bytes.
func (f *Frame) Decode(out []byte) {
	out = out[:f.header.BytesPerFrame()]
	nch := f.header.NumberOfChannels()
	for gr := range f.header.Granules() {
		g := &f.g.Ch[gr]
		for ch := range nch {
			f.requantize(&g[ch])
			f.reorder(&g[ch])
		}
		f.stereo(g)
		for ch := range nch {
			antialias(&g[ch])
			f.hybridSynthesis(&g[ch], ch)
			f.subbandSynthesis(&g[ch], ch, out[frameheader.SamplesPerGranule*4*gr:])
		}
	}
}

// requantizeBand turns the Huffman-decoded integers in is into frequency
// lines: sign(v) * |v|^(4/3) * gain.
func requantizeBand(is []float32, gain float64) {
	for i, v := range is {
		if v < 0 {
			is[i] = float32(gain * -powtab34[int(-v)])
		} else {
			is[i] = float32(gain * powtab34[int(v)])
		}
	}
}

// requantize applies the per-band gains to the lines below count1. Lines above
// it are zero and stay zero, so a band that straddles count1 is done whole.
func (f *Frame) requantize(c *granule.Channel) {
	long, short := f.g.Long, f.g.Short
	is := &c.Lines
	count1 := c.Count1

	if !c.ShortBlocks {
		for sfb := 0; long[sfb] < count1; sfb++ {
			requantizeBand(is[long[sfb]:min(long[sfb+1], count1)], c.GainLong[sfb])
		}
		return
	}

	// A mixed block codes its first two subbands (36 lines, 8 long bands for
	// MPEG1, 6 for MPEG2) as long, and the short bands start at sfb 3.
	firstShort := 0
	if c.Mixed {
		for sfb := 0; long[sfb] < 36; sfb++ {
			requantizeBand(is[long[sfb]:min(long[sfb+1], 36)], c.GainLong[sfb])
		}
		firstShort = 3
	}
	for sfb := firstShort; sfb < 13 && short[sfb]*3 < count1; sfb++ {
		winLen := short[sfb+1] - short[sfb]
		for win := range 3 {
			start := short[sfb]*3 + win*winLen
			requantizeBand(is[start:start+winLen], c.GainShort[sfb][win])
		}
	}
}

// reorder puts a short-block granule's lines in subband order: the bitstream
// carries each band's three windows in turn.
func (f *Frame) reorder(c *granule.Channel) {
	if !c.ShortBlocks {
		return
	}
	re := make([]float32, frameheader.SamplesPerGranule)
	short := f.g.Short
	is := &c.Lines

	// The first two subbands (8 long or 3 short sfbs) may use long blocks.
	sfb := 0
	if c.Mixed {
		sfb = 3
	}
	nextSfb := short[sfb+1] * 3
	winLen := short[sfb+1] - short[sfb]
	i := 36
	if sfb == 0 {
		i = 0
	}
	for i < frameheader.SamplesPerGranule {
		if i == nextSfb {
			j := 3 * short[sfb]
			copy(is[j:j+3*winLen], re[0:3*winLen])
			if i >= c.Count1 {
				return
			}
			sfb++
			nextSfb = short[sfb+1] * 3
			winLen = short[sfb+1] - short[sfb]
		}
		for win := range 3 {
			for j := range winLen {
				re[j*3+win] = is[i]
				i++
			}
		}
	}
	j := 3 * short[12]
	copy(is[j:j+3*winLen], re[0:3*winLen])
}

var isRatios = []float32{0.000000, 0.267949, 0.577350, 1.000000, 1.732051, 3.732051}

// intensityRatios maps an is_pos to the left and right scaling; is_pos 7 and
// above mean no intensity stereo for the band.
func intensityRatios(isPos int) (l, r float32, ok bool) {
	if isPos >= 7 {
		return 0, 0, false
	}
	if isPos == 6 { // tan((6*PI)/12 = PI/2) needs special treatment!
		return 1, 0, true
	}
	return isRatios[isPos] / (1.0 + isRatios[isPos]), 1.0 / (1.0 + isRatios[isPos]), true
}

func (f *Frame) stereoProcessIntensityLong(g *[2]granule.Channel, sfb int) {
	ratioL, ratioR, ok := intensityRatios(g[0].ScalefacL[sfb])
	if !ok {
		return
	}
	long := f.g.Long
	for i := long[sfb]; i < long[sfb+1]; i++ {
		g[0].Lines[i] *= ratioL
		g[1].Lines[i] *= ratioR
	}
}

func (f *Frame) stereoProcessIntensityShort(g *[2]granule.Channel, sfb int) {
	short := f.g.Short
	winLen := short[sfb+1] - short[sfb]
	// The three windows within the band have different scalefactors.
	for win := range 3 {
		ratioL, ratioR, ok := intensityRatios(g[0].ScalefacS[sfb][win])
		if !ok {
			continue
		}
		start := short[sfb]*3 + winLen*win
		for i := start; i < start+winLen; i++ {
			// https://github.com/technosaurus/PDMP3/issues/3
			g[0].Lines[i] *= ratioL
			g[1].Lines[i] *= ratioR
		}
	}
}

// stereo undoes mid/side and intensity joint-stereo coding across the two
// channels of a granule.
func (f *Frame) stereo(g *[2]granule.Channel) {
	l, r := &g[0], &g[1]
	if f.header.UseMSStereo() {
		const invSqrt2 = math.Sqrt2 / 2
		for i := range max(l.Count1, r.Count1) {
			left := (l.Lines[i] + r.Lines[i]) * invSqrt2
			right := (l.Lines[i] - r.Lines[i]) * invSqrt2
			l.Lines[i] = left
			r.Lines[i] = right
		}
	}

	if f.header.UseIntensityStereo() {
		long, short := f.g.Long, f.g.Short
		// Intensity stereo applies from the first scalefactor band on or above
		// the right channel's count1 line.
		switch {
		case l.ShortBlocks && l.Mixed:
			for sfb := range 8 {
				if long[sfb] >= r.Count1 {
					f.stereoProcessIntensityLong(g, sfb)
				}
			}
			for sfb := 3; sfb < 12; sfb++ {
				if short[sfb]*3 >= r.Count1 {
					f.stereoProcessIntensityShort(g, sfb)
				}
			}
		case l.ShortBlocks:
			for sfb := range 12 {
				if short[sfb]*3 >= r.Count1 {
					f.stereoProcessIntensityShort(g, sfb)
				}
			}
		default:
			for sfb := range 21 {
				if long[sfb] >= r.Count1 {
					f.stereoProcessIntensityLong(g, sfb)
				}
			}
		}
	}
}

var (
	cs = []float32{0.857493, 0.881742, 0.949629, 0.983315, 0.995518, 0.999161, 0.999899, 0.999993}
	ca = []float32{-0.514496, -0.471732, -0.313377, -0.181913, -0.094574, -0.040966, -0.014199, -0.003700}
)

// antialias runs the butterflies between adjacent subbands. Short blocks are
// left alone; a mixed block only has its two long subbands to treat.
func antialias(c *granule.Channel) {
	sblim := 32
	if c.ShortBlocks {
		if !c.Mixed {
			return
		}
		sblim = 2
	}
	is := &c.Lines
	for sb := 1; sb < sblim; sb++ {
		for i := range 8 {
			li := 18*sb - 1 - i
			ui := 18*sb + i
			lb := is[li]*cs[i] - is[ui]*ca[i]
			ub := is[ui]*cs[i] + is[li]*ca[i]
			is[li] = lb
			is[ui] = ub
		}
	}
}

// hybridSynthesis runs the IMDCT on every subband, overlap-adds the previous
// granule's second half, and applies the frequency inversion: odd lines of
// odd subbands are negated to undo the polyphase filterbank's aliasing sign.
//
//nolint:gosec // fixed-size arrays; every index below is provably in range
func (f *Frame) hybridSynthesis(c *granule.Channel, ch int) {
	var rawout [36]float32
	for sb := range 32 {
		bt := c.BlockType
		if c.Mixed && sb < 2 {
			bt = 0
		}
		lines := (*[18]float32)(c.Lines[sb*18 : sb*18+18])
		store := &f.store[ch][sb]
		imdct.Win(&rawout, lines, bt)
		for i := range 18 {
			lines[i] = rawout[i] + store[i]
			store[i] = rawout[i+18]
		}
		if sb%2 == 1 {
			for i := 1; i < 18; i += 2 {
				lines[i] = -lines[i]
			}
		}
	}
}

var synthDtbl = [512]float32{
	0.000000000, -0.000015259, -0.000015259, -0.000015259,
	-0.000015259, -0.000015259, -0.000015259, -0.000030518,
	-0.000030518, -0.000030518, -0.000030518, -0.000045776,
	-0.000045776, -0.000061035, -0.000061035, -0.000076294,
	-0.000076294, -0.000091553, -0.000106812, -0.000106812,
	-0.000122070, -0.000137329, -0.000152588, -0.000167847,
	-0.000198364, -0.000213623, -0.000244141, -0.000259399,
	-0.000289917, -0.000320435, -0.000366211, -0.000396729,
	-0.000442505, -0.000473022, -0.000534058, -0.000579834,
	-0.000625610, -0.000686646, -0.000747681, -0.000808716,
	-0.000885010, -0.000961304, -0.001037598, -0.001113892,
	-0.001205444, -0.001296997, -0.001388550, -0.001480103,
	-0.001586914, -0.001693726, -0.001785278, -0.001907349,
	-0.002014160, -0.002120972, -0.002243042, -0.002349854,
	-0.002456665, -0.002578735, -0.002685547, -0.002792358,
	-0.002899170, -0.002990723, -0.003082275, -0.003173828,
	0.003250122, 0.003326416, 0.003387451, 0.003433228,
	0.003463745, 0.003479004, 0.003479004, 0.003463745,
	0.003417969, 0.003372192, 0.003280640, 0.003173828,
	0.003051758, 0.002883911, 0.002700806, 0.002487183,
	0.002227783, 0.001937866, 0.001617432, 0.001266479,
	0.000869751, 0.000442505, -0.000030518, -0.000549316,
	-0.001098633, -0.001693726, -0.002334595, -0.003005981,
	-0.003723145, -0.004486084, -0.005294800, -0.006118774,
	-0.007003784, -0.007919312, -0.008865356, -0.009841919,
	-0.010848999, -0.011886597, -0.012939453, -0.014022827,
	-0.015121460, -0.016235352, -0.017349243, -0.018463135,
	-0.019577026, -0.020690918, -0.021789551, -0.022857666,
	-0.023910522, -0.024932861, -0.025909424, -0.026840210,
	-0.027725220, -0.028533936, -0.029281616, -0.029937744,
	-0.030532837, -0.031005859, -0.031387329, -0.031661987,
	-0.031814575, -0.031845093, -0.031738281, -0.031478882,
	0.031082153, 0.030517578, 0.029785156, 0.028884888,
	0.027801514, 0.026535034, 0.025085449, 0.023422241,
	0.021575928, 0.019531250, 0.017257690, 0.014801025,
	0.012115479, 0.009231567, 0.006134033, 0.002822876,
	-0.000686646, -0.004394531, -0.008316040, -0.012420654,
	-0.016708374, -0.021179199, -0.025817871, -0.030609131,
	-0.035552979, -0.040634155, -0.045837402, -0.051132202,
	-0.056533813, -0.061996460, -0.067520142, -0.073059082,
	-0.078628540, -0.084182739, -0.089706421, -0.095169067,
	-0.100540161, -0.105819702, -0.110946655, -0.115921021,
	-0.120697021, -0.125259399, -0.129562378, -0.133590698,
	-0.137298584, -0.140670776, -0.143676758, -0.146255493,
	-0.148422241, -0.150115967, -0.151306152, -0.151962280,
	-0.152069092, -0.151596069, -0.150497437, -0.148773193,
	-0.146362305, -0.143264771, -0.139450073, -0.134887695,
	-0.129577637, -0.123474121, -0.116577148, -0.108856201,
	0.100311279, 0.090927124, 0.080688477, 0.069595337,
	0.057617188, 0.044784546, 0.031082153, 0.016510010,
	0.001068115, -0.015228271, -0.032379150, -0.050354004,
	-0.069168091, -0.088775635, -0.109161377, -0.130310059,
	-0.152206421, -0.174789429, -0.198059082, -0.221984863,
	-0.246505737, -0.271591187, -0.297210693, -0.323318481,
	-0.349868774, -0.376800537, -0.404083252, -0.431655884,
	-0.459472656, -0.487472534, -0.515609741, -0.543823242,
	-0.572036743, -0.600219727, -0.628295898, -0.656219482,
	-0.683914185, -0.711318970, -0.738372803, -0.765029907,
	-0.791213989, -0.816864014, -0.841949463, -0.866363525,
	-0.890090942, -0.913055420, -0.935195923, -0.956481934,
	-0.976852417, -0.996246338, -1.014617920, -1.031936646,
	-1.048156738, -1.063217163, -1.077117920, -1.089782715,
	-1.101211548, -1.111373901, -1.120223999, -1.127746582,
	-1.133926392, -1.138763428, -1.142211914, -1.144287109,
	1.144989014, 1.144287109, 1.142211914, 1.138763428,
	1.133926392, 1.127746582, 1.120223999, 1.111373901,
	1.101211548, 1.089782715, 1.077117920, 1.063217163,
	1.048156738, 1.031936646, 1.014617920, 0.996246338,
	0.976852417, 0.956481934, 0.935195923, 0.913055420,
	0.890090942, 0.866363525, 0.841949463, 0.816864014,
	0.791213989, 0.765029907, 0.738372803, 0.711318970,
	0.683914185, 0.656219482, 0.628295898, 0.600219727,
	0.572036743, 0.543823242, 0.515609741, 0.487472534,
	0.459472656, 0.431655884, 0.404083252, 0.376800537,
	0.349868774, 0.323318481, 0.297210693, 0.271591187,
	0.246505737, 0.221984863, 0.198059082, 0.174789429,
	0.152206421, 0.130310059, 0.109161377, 0.088775635,
	0.069168091, 0.050354004, 0.032379150, 0.015228271,
	-0.001068115, -0.016510010, -0.031082153, -0.044784546,
	-0.057617188, -0.069595337, -0.080688477, -0.090927124,
	0.100311279, 0.108856201, 0.116577148, 0.123474121,
	0.129577637, 0.134887695, 0.139450073, 0.143264771,
	0.146362305, 0.148773193, 0.150497437, 0.151596069,
	0.152069092, 0.151962280, 0.151306152, 0.150115967,
	0.148422241, 0.146255493, 0.143676758, 0.140670776,
	0.137298584, 0.133590698, 0.129562378, 0.125259399,
	0.120697021, 0.115921021, 0.110946655, 0.105819702,
	0.100540161, 0.095169067, 0.089706421, 0.084182739,
	0.078628540, 0.073059082, 0.067520142, 0.061996460,
	0.056533813, 0.051132202, 0.045837402, 0.040634155,
	0.035552979, 0.030609131, 0.025817871, 0.021179199,
	0.016708374, 0.012420654, 0.008316040, 0.004394531,
	0.000686646, -0.002822876, -0.006134033, -0.009231567,
	-0.012115479, -0.014801025, -0.017257690, -0.019531250,
	-0.021575928, -0.023422241, -0.025085449, -0.026535034,
	-0.027801514, -0.028884888, -0.029785156, -0.030517578,
	0.031082153, 0.031478882, 0.031738281, 0.031845093,
	0.031814575, 0.031661987, 0.031387329, 0.031005859,
	0.030532837, 0.029937744, 0.029281616, 0.028533936,
	0.027725220, 0.026840210, 0.025909424, 0.024932861,
	0.023910522, 0.022857666, 0.021789551, 0.020690918,
	0.019577026, 0.018463135, 0.017349243, 0.016235352,
	0.015121460, 0.014022827, 0.012939453, 0.011886597,
	0.010848999, 0.009841919, 0.008865356, 0.007919312,
	0.007003784, 0.006118774, 0.005294800, 0.004486084,
	0.003723145, 0.003005981, 0.002334595, 0.001693726,
	0.001098633, 0.000549316, 0.000030518, -0.000442505,
	-0.000869751, -0.001266479, -0.001617432, -0.001937866,
	-0.002227783, -0.002487183, -0.002700806, -0.002883911,
	-0.003051758, -0.003173828, -0.003280640, -0.003372192,
	-0.003417969, -0.003463745, -0.003479004, -0.003479004,
	-0.003463745, -0.003433228, -0.003387451, -0.003326416,
	0.003250122, 0.003173828, 0.003082275, 0.002990723,
	0.002899170, 0.002792358, 0.002685547, 0.002578735,
	0.002456665, 0.002349854, 0.002243042, 0.002120972,
	0.002014160, 0.001907349, 0.001785278, 0.001693726,
	0.001586914, 0.001480103, 0.001388550, 0.001296997,
	0.001205444, 0.001113892, 0.001037598, 0.000961304,
	0.000885010, 0.000808716, 0.000747681, 0.000686646,
	0.000625610, 0.000579834, 0.000534058, 0.000473022,
	0.000442505, 0.000396729, 0.000366211, 0.000320435,
	0.000289917, 0.000259399, 0.000244141, 0.000213623,
	0.000198364, 0.000167847, 0.000152588, 0.000137329,
	0.000122070, 0.000106812, 0.000106812, 0.000091553,
	0.000076294, 0.000076294, 0.000061035, 0.000061035,
	0.000045776, 0.000045776, 0.000030518, 0.000030518,
	0.000030518, 0.000030518, 0.000015259, 0.000015259,
	0.000015259, 0.000015259, 0.000015259, 0.000015259,
}

func (f *Frame) subbandSynthesis(c *granule.Channel, ch int, out []byte) {
	// Scratch for the 32 subband samples of one time slot.
	var sVec [32]float32

	v := &f.vVec[ch]
	p := f.vOff[ch]
	nch := f.header.NumberOfChannels()
	for ss := range 18 { // Loop through 18 samples in 32 subbands
		// Make room for 64 new values by moving the window instead of the
		// history: p stays a multiple of 64, so the new block never wraps.
		p = (p - 64) & 1023
		d := &c.Lines
		for i := range 32 { // Copy next 32 time samples to a temp vector
			sVec[i] = d[i*18+ss] //nolint:gosec // i is 0-31 and ss is 0-17, so max index is 31*18+17=575 < 576
		}
		synthesisMatrix(&sVec, (*[64]float32)(v[p:p+64])) // The ISO n_win matrixing

		// Build, window and sum in one pass over the 16 32-value runs the U
		// vector was assembled from: each of its 512 entries was read exactly
		// once, so materialising it only cost a store and a load per entry.
		// Runs start on a multiple of 32, so none of them wraps, and q ascending
		// is the order the separate sum used, so per output sample the additions
		// still happen in the same sequence.
		//
		// The 32 accumulators live in memory on purpose: with q outer they are
		// independent and the loop runs at load/store throughput. Keeping one
		// output's sum in a register (i outer, 16 taps) makes it a dependency
		// chain and measured 17% slower; unrolling i by 4 measured no change
		// (#19). What is left here is SIMD.
		var sums [32]float32
		for q := range 16 {
			base := (p + 128*(q/2) + 96*(q%2)) & 1023
			src := (*[32]float32)(v[base : base+32])
			win := (*[32]float32)(synthDtbl[32*q : 32*q+32])
			for i := range sums {
				sums[i] += src[i] * win[i]
			}
		}

		for i, sum := range sums { // Calc 32 samples,store in outdata vector
			// sum is time sample 32*ss+i. Convert to 16-bit signed int
			samp := int(sum * 32767)
			if samp > 32767 {
				samp = 32767
			} else if samp < -32767 {
				samp = -32767
			}
			s := int16(samp) //nolint:gosec // samp is clamped to [-32767, 32767] above
			idx := 4 * (32*ss + i)
			if nch == 1 {
				// We always run in stereo mode and duplicate channels here for mono.
				out[idx] = byte(s)
				out[idx+1] = byte(s >> 8)
				out[idx+2] = byte(s)
				out[idx+3] = byte(s >> 8)
				continue
			}
			if ch == 0 {
				out[idx] = byte(s)
				out[idx+1] = byte(s >> 8)
			} else {
				out[idx+2] = byte(s)
				out[idx+3] = byte(s >> 8)
			}
		}
	}
	f.vOff[ch] = p
}
