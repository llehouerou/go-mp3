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

import (
	"encoding/binary"
	"errors"
)

// ErrOutOfBounds is returned when attempting to read past the end of the buffer.
var ErrOutOfBounds = errors.New("bits: read past end of buffer")

// Bits reads a byte slice MSB first. Reads past the end return zero and set a
// sticky error.
type Bits struct {
	vec []byte
	pos int // in bits
	err error
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

// Shift discards all but the last keep bytes and rewinds to the first bit,
// forgetting any earlier read error.
func (b *Bits) Shift(keep int) {
	copy(b.vec, b.vec[len(b.vec)-keep:])
	b.vec = b.vec[:keep]
	b.pos, b.err = 0, nil
}

// Grow appends n zero bytes and returns them for the caller to fill.
func (b *Bits) Grow(n int) []byte {
	b.vec = append(b.vec, make([]byte, n)...)
	return b.vec[len(b.vec)-n:]
}

func (b *Bits) Bit() int {
	i := b.pos >> 3
	if i >= len(b.vec) {
		b.err = ErrOutOfBounds
		return 0
	}
	v := int(b.vec[i]>>(7-b.pos&7)) & 1
	b.pos++
	return v
}

// Bits reads the next num bits, num at most 57, as an unsigned integer.
func (b *Bits) Bits(num int) int {
	if num == 0 {
		return 0
	}
	if b.pos+num > len(b.vec)*8 {
		b.err = ErrOutOfBounds
		return 0
	}
	i := b.pos >> 3
	var w uint64
	if i+8 <= len(b.vec) {
		w = binary.BigEndian.Uint64(b.vec[i:])
	} else {
		for j, c := range b.vec[i:] {
			w |= uint64(c) << (56 - 8*j)
		}
	}
	w <<= b.pos & 7
	b.pos += num
	return int(w >> (64 - num)) //nolint:gosec // at most num <= 57 bits remain
}

func (b *Bits) BitPos() int {
	return b.pos
}

func (b *Bits) SetPos(pos int) {
	b.pos = pos
}

func (b *Bits) LenInBytes() int {
	return len(b.vec)
}
