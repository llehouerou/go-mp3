package bits_test

import (
	"testing"

	"github.com/llehouerou/go-mp3/internal/bits"
)

func TestBitsMSBFirst(t *testing.T) {
	b := bits.New([]byte{0b01010101, 0b10101010, 0b11001100, 0b00110011})
	for i, want := range []int{0, 1, 0, 1} {
		if got := b.Bits(1); got != want {
			t.Fatalf("bit %d = %d, want %d", i, got, want)
		}
	}
	if got := b.Bits(8); got != 0b01011010 {
		t.Fatalf("Bits(8) = %#b, want 01011010", got)
	}
	if got := b.Peek(12); got != 0b101011001100 {
		t.Fatalf("Peek(12) = %#b", got)
	}
	if got := b.Bits(12); got != 0b101011001100 {
		t.Fatalf("Bits(12) = %#b", got)
	}
	if b.BitPos() != 24 {
		t.Fatalf("BitPos() = %d, want 24", b.BitPos())
	}
}

// TestPastTheEndReadsZero pins the contract every caller relies on: the bit
// reservoir may be shorter than a frame's main_data_begin claims, and Huffman
// decoding runs to a bit count, not to a buffer end. Reads past the end
// yield zero and leave the position where it was; Skip stops at the end.
func TestPastTheEndReadsZero(t *testing.T) {
	b := bits.New([]byte{0xff, 0xff})
	b.Skip(12)
	if got := b.Bits(8); got != 0 || b.BitPos() != 12 {
		t.Fatalf("Bits(8) over the end = %d at pos %d, want 0 at 12", got, b.BitPos())
	}
	if got := b.Peek(8); got != 0b11110000 {
		t.Fatalf("Peek(8) over the end = %#b, want 11110000", got)
	}
	if got := b.Bits(4); got != 0xf || b.BitPos() != 16 {
		t.Fatalf("Bits(4) to the end = %#x at pos %d, want f at 16", got, b.BitPos())
	}
	if got := b.Bit(); got != 0 || b.BitPos() != 16 {
		t.Fatalf("Bit() past the end = %d at pos %d, want 0 at 16", got, b.BitPos())
	}
	b.SetPos(8)
	b.Skip(100)
	if b.BitPos() != 16 {
		t.Fatalf("Skip past the end left pos at %d, want 16", b.BitPos())
	}
}
