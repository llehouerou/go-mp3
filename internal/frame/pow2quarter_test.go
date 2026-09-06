package frame

import (
	"math"
	"testing"
)

// The requantization gain table must stay bit-identical to math.Pow(2, k/4),
// which is what the decoder computed before it was precomputed.
func TestPow2QuarterIsBitIdenticalToPow(t *testing.T) {
	for i := range pow2Quarter {
		k := i + pow2QuarterMin
		want := math.Pow(2.0, float64(k)/4.0)
		if pow2Quarter[i] != want {
			t.Fatalf("pow2Quarter[%d] (k=%d) = %v, want %v", i, k, pow2Quarter[i], want)
		}
	}
}
