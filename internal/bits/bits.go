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

package bits

import "errors"

// ErrOutOfBounds is returned when attempting to read past the end of the buffer.
var ErrOutOfBounds = errors.New("bits: read past end of buffer")

type Bits struct {
	vec     []byte
	bitPos  int
	bytePos int
	err     error
}

// Err returns any error that occurred during bit reading operations.
// Once an error occurs, subsequent reads will continue to return the error.
func (b *Bits) Err() error {
	return b.err
}

func New(vec []byte) *Bits {
	return &Bits{
		vec: vec,
	}
}

func Append(bits *Bits, buf []byte) *Bits {
	return New(append(bits.vec, buf...))
}

func (b *Bits) Bit() int {
	if len(b.vec) <= b.bytePos {
		b.err = ErrOutOfBounds
		return 0
	}
	// bitPos is always 0-7 (controlled by modulo 8 arithmetic), so conversion is safe
	tmp := uint(b.vec[b.bytePos]) >> (7 - uint(b.bitPos)) //nolint:gosec // bitPos is always 0-7
	tmp &= 0x01
	b.bytePos += (b.bitPos + 1) >> 3
	b.bitPos = (b.bitPos + 1) & 0x07
	return int(tmp) //nolint:gosec // tmp is always 0-1
}

func (b *Bits) Bits(num int) int {
	if num == 0 {
		return 0
	}
	// Check if we have enough bits remaining
	currentBitPos := b.bytePos*8 + b.bitPos
	totalBits := len(b.vec) * 8
	if currentBitPos+num > totalBits {
		b.err = ErrOutOfBounds
		return 0
	}
	bb := make([]byte, 4)
	copy(bb, b.vec[b.bytePos:])
	tmp := (uint32(bb[0]) << 24) | (uint32(bb[1]) << 16) | (uint32(bb[2]) << 8) | (uint32(bb[3]))
	tmp <<= uint(b.bitPos)   //nolint:gosec // bitPos is always 0-7, safe for uint conversion
	tmp >>= (32 - uint(num)) //nolint:gosec // num is always 0-32 for MP3 parsing
	b.bytePos += (b.bitPos + num) >> 3
	b.bitPos = (b.bitPos + num) & 0x07
	return int(tmp) //nolint:gosec // tmp fits in int after right shift
}

func (b *Bits) BitPos() int {
	return b.bytePos<<3 + b.bitPos
}

func (b *Bits) SetPos(pos int) {
	b.bytePos = pos >> 3
	b.bitPos = pos & 0x7
}

func (b *Bits) LenInBytes() int {
	return len(b.vec)
}

func (b *Bits) Tail(offset int) []byte {
	return b.vec[len(b.vec)-offset:]
}
