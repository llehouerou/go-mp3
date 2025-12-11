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

package bits_test

import (
	"testing"

	"github.com/llehouerou/go-mp3/internal/bits"
)

func TestBit_OutOfBounds_ShouldReportError(t *testing.T) {
	// Create a 2-byte buffer
	b := bits.New([]byte{0xFF, 0xFF}) // 16 bits total

	// Read all 16 bits - should succeed
	for i := range 16 {
		_ = b.Bit()
		if b.Err() != nil {
			t.Fatalf("unexpected error after reading bit %d: %v", i, b.Err())
		}
	}

	// Now we're at the end. Reading another bit should indicate an error.
	val := b.Bit()
	if b.Err() == nil {
		t.Errorf("expected error after reading past buffer, got value %d with no error", val)
	}
}

func TestBits_OutOfBounds_ShouldReportError(t *testing.T) {
	// Create a 2-byte buffer (16 bits)
	b := bits.New([]byte{0xAB, 0xCD})

	// Read 8 bits - should succeed
	val := b.Bits(8)
	if b.Err() != nil {
		t.Fatalf("unexpected error reading first 8 bits: %v", b.Err())
	}
	if val != 0xAB {
		t.Errorf("expected 0xAB, got 0x%X", val)
	}

	// Read another 8 bits - should succeed (exactly at end)
	val = b.Bits(8)
	if b.Err() != nil {
		t.Fatalf("unexpected error reading last 8 bits: %v", b.Err())
	}
	if val != 0xCD {
		t.Errorf("expected 0xCD, got 0x%X", val)
	}

	// Now read past the buffer - should indicate an error
	val = b.Bits(8)
	if b.Err() == nil {
		t.Errorf("expected error after reading past buffer, got value %d with no error", val)
	}
}

func TestBits_PartialOutOfBounds_ShouldReportError(t *testing.T) {
	// Create a 1-byte buffer (8 bits)
	b := bits.New([]byte{0xFF})

	// Read 4 bits - should succeed
	_ = b.Bits(4)
	if b.Err() != nil {
		t.Fatalf("unexpected error reading first 4 bits: %v", b.Err())
	}

	// Try to read 8 more bits (only 4 available) - should indicate error
	val := b.Bits(8)
	if b.Err() == nil {
		t.Errorf("expected error when reading 8 bits with only 4 available, got value %d", val)
	}
}

func TestBits(t *testing.T) {
	b1 := byte(85)  // 01010101
	b2 := byte(170) // 10101010
	b3 := byte(204) // 11001100
	b4 := byte(51)  // 00110011
	b := bits.New([]byte{b1, b2, b3, b4})
	if b.Bits(1) != 0 {
		t.Fail()
	}
	if b.Bits(1) != 1 {
		t.Fail()
	}
	if b.Bits(1) != 0 {
		t.Fail()
	}
	if b.Bits(1) != 1 {
		t.Fail()
	}
	if b.Bits(8) != 90 /* 01011010 */ {
		t.Fail()
	}
	if b.Bits(12) != 2764 /* 101011001100 */ {
		t.Fail()
	}
}
