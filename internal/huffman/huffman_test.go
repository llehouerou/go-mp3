package huffman

import (
	"testing"

	"github.com/llehouerou/go-mp3/internal/bits"
)

type leaf struct {
	code  uint32 // codeword bits, MSB first
	depth int
	value uint16 // x<<4 | y
}

// leaves lists every codeword of a table by walking its tree.
func leaves(ht []uint16, treelen int) []leaf {
	var out []leaf
	var visit func(point int, code uint32, depth int)
	visit = func(point int, code uint32, depth int) {
		if point >= treelen {
			return
		}
		if ht[point]&0xff00 == 0 {
			out = append(out, leaf{code, depth, ht[point] & 0xff})
			return
		}
		visit(child(ht, point, 0), code<<1, depth+1)
		visit(child(ht, point, 1), code<<1|1, depth+1)
	}
	visit(0, 0, 0)
	return out
}

// TestDecodeEveryCodeword checks Decode against the tree for every codeword of
// every table, so both the lookup path and the walk fallback are covered.
func TestDecodeEveryCodeword(t *testing.T) {
	for tableNum, table := range &huffmanMain {
		if table.treelen == 0 {
			continue
		}
		short, long := 0, 0
		for _, l := range leaves(table.hufftable, table.treelen) {
			if l.depth <= lutBits {
				short++
			} else {
				long++
			}
			// Codeword, left-aligned, then zero padding: no linbits, no sign.
			buf := make([]byte, 8)
			w := uint64(l.code) << (64 - l.depth)
			for i := range buf {
				buf[i] = byte(w >> (56 - 8*i))
			}
			m := bits.New(buf)
			x, y, v, wv, err := Decode(&m, tableNum)
			if err != nil {
				t.Fatalf("table %d code %0*b: %v", tableNum, l.depth, l.code, err)
			}
			wantX, wantY := int(l.value>>4)&0xf, int(l.value)&0xf
			if tableNum > 31 {
				wantV, wantW := (wantY>>3)&1, (wantY>>2)&1
				wantX, wantY = (wantY>>1)&1, wantY&1
				if v != wantV || wv != wantW {
					t.Errorf("table %d code %0*b: v,w = %d,%d want %d,%d", tableNum, l.depth, l.code, v, wv, wantV, wantW)
				}
			}
			if x != wantX || y != wantY {
				t.Errorf("table %d code %0*b: x,y = %d,%d want %d,%d", tableNum, l.depth, l.code, x, y, wantX, wantY)
			}
			// Position: the codeword, plus linbits (read as zero) for an escaped
			// value, plus a sign bit per nonzero value.
			want := l.depth
			if x != 0 {
				want++
			}
			if y != 0 {
				want++
			}
			if x == 15 {
				want += table.linbits
			}
			if y == 15 {
				want += table.linbits
			}
			if tableNum > 31 {
				want = l.depth + v + wv + x + y
			}
			if m.BitPos() != want {
				t.Errorf("table %d code %0*b: consumed %d bits, want %d", tableNum, l.depth, l.code, m.BitPos(), want)
			}
		}
		if short == 0 {
			t.Errorf("table %d: no codeword within lutBits", tableNum)
		}
		if tableNum == 13 && long == 0 {
			t.Errorf("table 13: expected codewords longer than lutBits to exercise the walk")
		}
	}
}
