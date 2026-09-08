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

// Package bits reads a byte slice bit by bit, MSB first.
package bits

import "encoding/binary"

// Bits reads a byte slice MSB first. Reads past the end yield zero and leave
// the position where it was: the callers decode to a bit count carried in
// the stream, not to the end of a buffer, so running out of bytes is not an
// error at this level.
type Bits struct {
	vec []byte
	pos int // in bits
}

// New returns a reader positioned at the first bit of vec.
func New(vec []byte) Bits {
	return Bits{vec: vec}
}

func (b *Bits) Bit() int {
	i := b.pos >> 3
	if i >= len(b.vec) {
		return 0
	}
	v := int(b.vec[i]>>(7-b.pos&7)) & 1
	b.pos++
	return v
}

// window returns the next 64 bits, MSB first, zero-padded past the end.
func (b *Bits) window() uint64 {
	i := b.pos >> 3
	if i >= len(b.vec) {
		return 0
	}
	var w uint64
	if i+8 <= len(b.vec) {
		w = binary.BigEndian.Uint64(b.vec[i:])
	} else {
		for j, c := range b.vec[i:] {
			w |= uint64(c) << (56 - 8*j)
		}
	}
	return w << (b.pos & 7)
}

// Bits reads the next num bits, num at most 57, as an unsigned integer.
func (b *Bits) Bits(num int) int {
	if num == 0 {
		return 0
	}
	if b.pos+num > len(b.vec)*8 {
		return 0
	}
	w := b.window()
	b.pos += num
	return int(w >> (64 - num)) //nolint:gosec // at most num <= 57 bits remain
}

// Peek returns the next num bits, num at most 57, without consuming them.
// Bits past the end read as zero, as they do bit by bit.
func (b *Bits) Peek(num int) int {
	return int(b.window() >> (64 - num)) //nolint:gosec // at most num <= 57 bits remain
}

// Skip consumes num bits, stopping at the end of the buffer exactly as num
// calls to Bit would.
func (b *Bits) Skip(num int) {
	b.pos = min(b.pos+num, len(b.vec)*8)
}

func (b *Bits) BitPos() int {
	return b.pos
}

func (b *Bits) SetPos(pos int) {
	b.pos = pos
}
