// Package granule turns a frame's bitstream into the numbers the DSP stages
// consume: for every granule and channel, the 576 Huffman-decoded frequency
// lines and the per-band requantization gains, block shape and scalefactors
// that go with them.
package granule

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/llehouerou/go-mp3/internal/bits"
	"github.com/llehouerou/go-mp3/internal/frameheader"
	"github.com/llehouerou/go-mp3/internal/huffman"
)

type FullReader interface {
	ReadFull([]byte) (int, error)
}

// Channel is one granule of one channel, ready for the DSP stages.
type Channel struct {
	// Lines are the Huffman-decoded frequency lines; every DSP stage rewrites
	// them in place. Lines[Count1:] are zero.
	Lines [frameheader.SamplesPerGranule]float32
	// Count1 is the first line of the all-zero region.
	Count1 int

	// ShortBlocks reports a window-switched short-block granule (BlockType 2).
	// Mixed marks a window-switched granule whose first two subbands keep long
	// blocks; encoders only set it with BlockType 2, but the IMDCT honours it
	// for any window-switched granule.
	ShortBlocks bool
	Mixed       bool
	// BlockType selects the IMDCT window, 0 to 3.
	BlockType int

	// GainLong and GainShort are the requantization gains 2^(k/4) per long
	// scalefactor band, and per short band and window.
	GainLong  [22]float64
	GainShort [13][3]float64

	// ScalefacL and ScalefacS are the raw scalefactors; intensity stereo reads
	// them as is_pos. They are already folded into the gains.
	ScalefacL [22]int
	ScalefacS [13][3]int

	// side info consumed by the parse only
	part2_3Length     int
	bigValues         int
	globalGain        int
	scalefacCompress  int
	winSwitchFlag     int
	mixedBlockFlag    int
	tableSelect       [3]int
	subblockGain      [3]int
	region0Count      int
	region1Count      int
	preflag           int
	scalefacScale     int
	count1TableSelect int
}

// A Reader turns frames into granules. It carries the bit reservoir from one
// frame to the next, so one Reader serves one stream; its zero value has an
// empty reservoir, and zeroing it forgets the reservoir again.
type Reader struct {
	// Long and Short are the scalefactor band index tables for the frame most
	// recently read.
	Long, Short []int
	// Ch holds the frame's granules, indexed [gr][ch]. Only the granules and
	// channels the header declares are written; the rest are stale.
	Ch [2][2]Channel

	// reservoir holds this frame's main data behind the tail of earlier
	// frames' that main_data_begin points back into; res reads it.
	reservoir     []byte
	res           bits.Bits
	mainDataBegin int
	scfsi         [2][4]int
	sideBuf       [32]byte
}

// Read parses the frame whose header h was just read from source: CRC, side
// information, main data through the reservoir, scalefactors and Huffman
// data, for every granule and channel. A source that runs out mid-frame gives
// a *frameheader.UnexpectedEOFError; a malformed frame gives another error. In both
// cases Ch is undefined and the frame must be dropped.
func (r *Reader) Read(source FullReader, h frameheader.FrameHeader) error {
	if h.ProtectionBit() == 0 {
		if err := readCRC(source); err != nil {
			return err
		}
	}
	framesize, err := h.FrameSize()
	if err != nil {
		return err
	}
	if framesize > 2000 {
		return fmt.Errorf("mp3: framesize = %d", framesize)
	}

	lsf := h.LowSamplingFrequency()
	bands := &sfBands[lsf][h.SamplingFrequency()]
	r.Long, r.Short = bands.long, bands.short

	if err := r.readSideInfo(source, h); err != nil {
		return err
	}

	// Main data is the rest of the frame, including ancillary data.
	mainDataSize := framesize - h.SideInfoSize() - 4
	if h.ProtectionBit() == 0 {
		mainDataSize -= 2
	}
	if err := r.fillReservoir(source, mainDataSize); err != nil {
		return err
	}

	nch := h.NumberOfChannels()
	if lsf == 1 {
		for ch := range nch {
			c := &r.Ch[0][ch]
			part2Start := r.res.BitPos()
			r.readScalefactorsMpeg2(c)
			if err := r.readHuffman(c, part2Start); err != nil {
				return err
			}
			c.finish()
		}
		return nil
	}
	for gr := range 2 {
		for ch := range nch {
			c := &r.Ch[gr][ch]
			part2Start := r.res.BitPos()
			r.readScalefactorsMpeg1(c, &r.Ch[0][ch], gr, ch)
			if err := r.readHuffman(c, part2Start); err != nil {
				return err
			}
			c.finish()
		}
	}
	return nil
}

// truncated reports a short read that ran into the end of the source, in
// either of the shapes a FullReader may report it.
func truncated(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func readCRC(source FullReader) error {
	buf := make([]byte, 2)
	if n, err := source.ReadFull(buf); n < 2 {
		if truncated(err) {
			return &frameheader.UnexpectedEOFError{At: "readCRC"}
		}
		return fmt.Errorf("mp3: error at readCRC: %w", err)
	}
	return nil
}

var sideInfoBitsToRead = [2][4]int{
	{9, 5, 3, 4}, // MPEG 1
	{8, 1, 2, 9}, // MPEG 2
}

// readSideInfo parses the side information block of the frame under h.
func (r *Reader) readSideInfo(source FullReader, h frameheader.FrameHeader) error {
	size := h.SideInfoSize()
	buf := r.sideBuf[:size]
	n, err := source.ReadFull(buf)
	if n < size {
		if truncated(err) {
			return &frameheader.UnexpectedEOFError{At: "sideinfo.Read"}
		}
		return fmt.Errorf("mp3: couldn't read sideinfo %d bytes: %w", size, err)
	}
	s := bits.New(buf)

	mpeg1 := h.LowSamplingFrequency() == 0
	bitsToRead := sideInfoBitsToRead[h.LowSamplingFrequency()]
	nch := h.NumberOfChannels()

	r.mainDataBegin = s.Bits(bitsToRead[0])
	// Private bits, unused.
	if h.Mode() == frameheader.ModeSingleChannel {
		s.Skip(bitsToRead[1])
	} else {
		s.Skip(bitsToRead[2])
	}
	r.scfsi = [2][4]int{}
	if mpeg1 {
		for ch := range nch {
			for band := range 4 {
				r.scfsi[ch][band] = s.Bits(1)
			}
		}
	}
	for gr := range h.Granules() {
		for ch := range nch {
			c := &r.Ch[gr][ch]
			c.part2_3Length = s.Bits(12)
			c.bigValues = s.Bits(9)
			c.globalGain = s.Bits(8)
			c.scalefacCompress = s.Bits(bitsToRead[3]) //nolint:gosec // bitsToRead is [4]int, index 3 is valid
			c.winSwitchFlag = s.Bits(1)
			if c.winSwitchFlag == 1 {
				c.BlockType = s.Bits(2)
				c.mixedBlockFlag = s.Bits(1)
				for region := range 2 {
					c.tableSelect[region] = s.Bits(5)
				}
				c.tableSelect[2] = 0
				for window := range 3 {
					c.subblockGain[window] = s.Bits(3)
				}
				// Implicit region counts; the standard is wrong on this.
				if c.BlockType == 2 && c.mixedBlockFlag == 0 {
					c.region0Count = 8
				} else {
					c.region0Count = 7
				}
				c.region1Count = 20 - c.region0Count
			} else {
				for region := range 3 {
					c.tableSelect[region] = s.Bits(5)
				}
				c.region0Count = s.Bits(4)
				c.region1Count = s.Bits(3)
				c.BlockType = 0
				c.mixedBlockFlag = 0
				c.subblockGain = [3]int{}
			}
			c.preflag = 0
			if mpeg1 {
				c.preflag = s.Bits(1)
			}
			c.scalefacScale = s.Bits(1)
			c.count1TableSelect = s.Bits(1)
		}
	}
	return nil
}

// fillReservoir appends this frame's main data to the reservoir, keeping the
// main_data_begin bytes of earlier frames it points back into. When the
// reservoir holds fewer (a stream joined mid-way), everything it has is kept
// and the frame decodes from that.
func (r *Reader) fillReservoir(source FullReader, size int) error {
	if size > 1500 {
		return fmt.Errorf("mp3: size = %d", size)
	}
	keep := min(r.mainDataBegin, len(r.reservoir))
	copy(r.reservoir, r.reservoir[len(r.reservoir)-keep:])
	r.reservoir = append(r.reservoir[:keep], make([]byte, size)...)
	if n, err := source.ReadFull(r.reservoir[keep:]); n < size {
		if truncated(err) {
			return &frameheader.UnexpectedEOFError{At: "maindata.Read"}
		}
		return err
	}
	r.res = bits.New(r.reservoir)
	return nil
}

var scalefacSizesMpeg1 = [16][2]int{
	{0, 0}, {0, 1}, {0, 2}, {0, 3}, {3, 0}, {1, 1}, {1, 2}, {1, 3},
	{2, 1}, {2, 2}, {2, 3}, {3, 1}, {3, 2}, {3, 3}, {4, 2}, {4, 3},
}

// readScalefactorsMpeg1 parses granule gr's scalefactors into c, copying the
// bands scfsi marks as shared from granule 0 (gr0) when gr is 1.
func (r *Reader) readScalefactorsMpeg1(c, gr0 *Channel, gr, ch int) {
	m := &r.res
	c.ScalefacL = [22]int{}
	c.ScalefacS = [13][3]int{}
	slen1 := scalefacSizesMpeg1[c.scalefacCompress][0]
	slen2 := scalefacSizesMpeg1[c.scalefacCompress][1]
	if c.winSwitchFlag == 1 && c.BlockType == 2 {
		if c.mixedBlockFlag != 0 {
			for sfb := range 8 {
				c.ScalefacL[sfb] = m.Bits(slen1)
			}
			for sfb := 3; sfb < 12; sfb++ {
				nbits := slen2
				if sfb < 6 {
					nbits = slen1
				}
				for win := range 3 {
					c.ScalefacS[sfb][win] = m.Bits(nbits)
				}
			}
		} else {
			for sfb := range 12 {
				nbits := slen2
				if sfb < 6 {
					nbits = slen1
				}
				for win := range 3 {
					c.ScalefacS[sfb][win] = m.Bits(nbits)
				}
			}
		}
		return
	}
	// Long blocks: four scfsi bands, each either read or shared with granule 0.
	bands := [5]int{0, 6, 11, 16, 21}
	for band := range 4 {
		nbits := slen1
		if band >= 2 {
			nbits = slen2
		}
		lo, hi := bands[band], bands[band+1]
		if r.scfsi[ch][band] == 0 || gr == 0 {
			for sfb := lo; sfb < hi; sfb++ {
				c.ScalefacL[sfb] = m.Bits(nbits)
			}
		} else {
			copy(c.ScalefacL[lo:hi], gr0.ScalefacL[lo:hi])
		}
	}
}

var scalefacSizesMpeg2 = [3][6][4]int{
	{{6, 5, 5, 5}, {6, 5, 7, 3}, {11, 10, 0, 0},
		{7, 7, 7, 0}, {6, 6, 6, 3}, {8, 8, 5, 0}},
	{{9, 9, 9, 9}, {9, 9, 12, 6}, {18, 18, 0, 0},
		{12, 12, 12, 0}, {12, 9, 9, 6}, {15, 12, 9, 0}},
	{{6, 9, 9, 9}, {6, 9, 12, 6}, {15, 18, 0, 0},
		{6, 15, 12, 0}, {6, 12, 9, 6}, {6, 18, 9, 0}}}

var nSlen2 = initSlen() // MPEG 2.0 slen for 'normal' mode

func initSlen() (nSlen2 [512]int) {
	for i := range 4 {
		for j := range 3 {
			n := j + i*3
			nSlen2[n+500] = i | (j << 3) | (2 << 12) | (1 << 15) //nolint:gosec // max n+500 = 2+9+500=511 < 512
		}
	}
	for i := range 5 {
		for j := range 5 {
			for k := range 4 {
				for l := range 4 {
					n := l + k*4 + j*16 + i*80
					nSlen2[n] = i | (j << 3) | (k << 6) | (l << 9) | (0 << 12) //nolint:gosec // max n = 3+12+64+320=399 < 512
				}
			}
		}
	}
	for i := range 5 {
		for j := range 5 {
			for k := range 4 {
				n := k + j*4 + i*20
				nSlen2[n+400] = i | (j << 3) | (k << 6) | (1 << 12) //nolint:gosec // max n+400 = 3+16+80+400=499 < 512
			}
		}
	}
	return
}

// readScalefactorsMpeg2 parses the single granule's scalefactors into c. The
// LSF layout also decides preflag, which MPEG-2 has no side-info bit for.
func (r *Reader) readScalefactorsMpeg2(c *Channel) {
	m := &r.res
	c.ScalefacL = [22]int{}
	c.ScalefacS = [13][3]int{}
	slen := nSlen2[c.scalefacCompress]
	c.preflag = (slen >> 15) & 0x1

	n := 0
	if c.BlockType == 2 {
		n++
		if c.mixedBlockFlag != 0 {
			n++
		}
	}
	d := (slen >> 12) & 0x7

	var sf [45]int
	count := 0
	for i := range 4 {
		num := slen & 0x7
		slen >>= 3
		for range scalefacSizesMpeg2[n][d][i] {
			if num > 0 {
				sf[count] = m.Bits(num)
			}
			count++
		}
	}
	count += (n << 1) + 1

	if count == 22 {
		copy(c.ScalefacL[:], sf[:22])
	} else {
		for x := range 13 {
			copy(c.ScalefacS[x][:], sf[x*3:x*3+3])
		}
	}
}

// readHuffman decodes the granule's Huffman data into c.Lines, starting at
// part2Start bits into the reservoir, and sets Count1.
func (r *Reader) readHuffman(c *Channel, part2Start int) error {
	m := &r.res
	is := &c.Lines
	if c.part2_3Length == 0 {
		*is = [frameheader.SamplesPerGranule]float32{}
		c.Count1 = 0
		return nil
	}

	bitPosEnd := part2Start + c.part2_3Length - 1
	region1Start := 0
	region2Start := 0
	if c.winSwitchFlag == 1 && c.BlockType == 2 {
		region1Start = 36                            // sfb[9/3]*3=36
		region2Start = frameheader.SamplesPerGranule // No Region2 for short block case.
	} else {
		l := r.Long
		i := c.region0Count + 1
		if i < 0 || len(l) <= i {
			return fmt.Errorf("mp3: readHuffman failed: invalid index i: %d", i)
		}
		region1Start = l[i]
		j := c.region0Count + c.region1Count + 2
		if j < 0 {
			return fmt.Errorf("mp3: readHuffman failed: invalid index j: %d", j)
		}
		// Clamp to the end of the scalefactor band table, as mpg123 and ffmpeg do.
		if j >= len(l) {
			region2Start = frameheader.SamplesPerGranule
		} else {
			region2Start = l[j]
		}
	}
	// big_values: two lines per Huffman word, table by region.
	for isPos := 0; isPos < c.bigValues*2; isPos++ {
		if isPos >= len(is) {
			return fmt.Errorf("mp3: isPos was too big: %d", isPos)
		}
		var tableNum int
		switch {
		case isPos < region1Start:
			tableNum = c.tableSelect[0]
		case isPos < region2Start:
			tableNum = c.tableSelect[1]
		default:
			tableNum = c.tableSelect[2]
		}
		x, y, _, _, err := huffman.Decode(m, tableNum)
		if err != nil {
			return err
		}
		is[isPos] = float32(x)
		isPos++
		is[isPos] = float32(y)
	}
	// count1: four lines per word until the lines or the bits run out.
	tableNum := c.count1TableSelect + 32
	isPos := c.bigValues * 2
	for isPos <= 572 && m.BitPos() <= bitPosEnd {
		x, y, v, w, err := huffman.Decode(m, tableNum)
		if err != nil {
			return err
		}
		is[isPos] = float32(v)
		isPos++
		if isPos >= frameheader.SamplesPerGranule {
			break
		}
		is[isPos] = float32(w)
		isPos++
		if isPos >= frameheader.SamplesPerGranule {
			break
		}
		is[isPos] = float32(x)
		isPos++
		if isPos >= frameheader.SamplesPerGranule {
			break
		}
		is[isPos] = float32(y)
		isPos++
	}
	// The last word may have overrun this granule's bits; drop it.
	if m.BitPos() > bitPosEnd+1 {
		isPos -= 4
	}
	c.Count1 = max(isPos, 0)
	for i := c.Count1; i < frameheader.SamplesPerGranule; i++ {
		is[i] = 0
	}
	m.SetPos(bitPosEnd + 1)
	return nil
}

// sfBands holds the scalefactor band start lines, indexed by MPEG-2 (LSF) and
// then by the header's sampling frequency field: 44.1, 48, 32 kHz for MPEG-1
// and 22.05, 24, 16 kHz for MPEG-2. Long bands are 22 entries plus the end of
// the granule; short bands 13 plus the end of one window (192 lines).
// ISO/IEC 11172-3 Table B.8 and ISO/IEC 13818-3 Table B.2.
var sfBands = [2][3]struct{ long, short []int }{
	{ // MPEG 1
		{ // 44.1 kHz
			[]int{0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 52, 62, 74, 90, 110, 134, 162, 196, 238, 288, 342, 418, 576},
			[]int{0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192},
		},
		{ // 48 kHz
			[]int{0, 4, 8, 12, 16, 20, 24, 30, 36, 42, 50, 60, 72, 88, 106, 128, 156, 190, 230, 276, 330, 384, 576},
			[]int{0, 4, 8, 12, 16, 22, 28, 38, 50, 64, 80, 100, 126, 192},
		},
		{ // 32 kHz
			[]int{0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 54, 66, 82, 102, 126, 156, 194, 240, 296, 364, 448, 550, 576},
			[]int{0, 4, 8, 12, 16, 22, 30, 42, 58, 78, 104, 138, 180, 192},
		},
	},
	{ // MPEG 2
		{ // 22.05 kHz
			[]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 116, 140, 168, 200, 238, 284, 336, 396, 464, 522, 576},
			[]int{0, 4, 8, 12, 18, 24, 32, 42, 56, 74, 100, 132, 174, 192},
		},
		{ // 24 kHz
			[]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 114, 136, 162, 194, 232, 278, 332, 394, 464, 540, 576},
			[]int{0, 4, 8, 12, 18, 26, 36, 48, 62, 80, 104, 136, 180, 192},
		},
		{ // 16 kHz
			[]int{0, 6, 12, 18, 24, 30, 36, 44, 54, 66, 80, 96, 116, 140, 168, 200, 238, 284, 336, 396, 464, 522, 576},
			[]int{0, 4, 8, 12, 18, 26, 36, 48, 62, 80, 104, 134, 174, 192},
		},
	},
}

// pow2QuarterMin is the smallest requantization exponent numerator k, where the
// gain factor is 2^(k/4). k = -m*(scalefac+preflag*pretab) + globalGain - 210 -
// 8*subblockGain, with m = 2 or 4. Every field is bit-width bounded by the
// parser: scalefac <= 15 (4 bits, MPEG1 and LSF alike), pretab <= 3,
// globalGain <= 255 (8 bits), subblockGain <= 7 (3 bits). So k ranges from
// -4*15 + 0 - 210 - 56 = -326 up to 0 + 255 - 210 = 45. An index outside that
// means the parser produced an out-of-spec field, and the panic is the right
// noise for it.
const (
	pow2QuarterMin = -326
	pow2QuarterLen = 45 - pow2QuarterMin + 1
)

var (
	pretab      = [22]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 3, 3, 3, 2, 0}
	pow2Quarter [pow2QuarterLen]float64
)

func init() {
	for i := range pow2Quarter {
		pow2Quarter[i] = math.Pow(2.0, float64(i+pow2QuarterMin)/4.0)
	}
}

// finish derives what the DSP stages read from the parsed fields: the block
// shape flags and the per-band gains.
func (c *Channel) finish() {
	c.ShortBlocks = c.winSwitchFlag == 1 && c.BlockType == 2
	c.Mixed = c.winSwitchFlag == 1 && c.mixedBlockFlag != 0

	m := 2
	if c.scalefacScale != 0 {
		m = 4
	}
	base := c.globalGain - 210
	for sfb := range c.GainLong {
		k := -m*(c.ScalefacL[sfb]+c.preflag*pretab[sfb]) + base
		c.GainLong[sfb] = pow2Quarter[k-pow2QuarterMin]
	}
	for sfb := range c.GainShort {
		for win := range 3 {
			k := -m*c.ScalefacS[sfb][win] + base - 8*c.subblockGain[win]
			c.GainShort[sfb][win] = pow2Quarter[k-pow2QuarterMin]
		}
	}
}
