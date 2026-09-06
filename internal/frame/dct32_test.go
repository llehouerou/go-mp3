package frame

import (
	"math"
	"math/rand"
	"testing"
)

// synthNWin is the ISO polyphase synthesis matrix, n[i][j] = cos((16+i)(2j+1)pi/64).
// It used to be evaluated directly in subbandSynthesis as a 64x32 multiply; it now
// lives here as the oracle the fast DCT must reproduce.
var synthNWin = func() (m [64][32]float32) {
	for i := range 64 {
		for j := range 32 {
			m[i][j] = float32(math.Cos(float64((16+i)*(2*j+1)) * (math.Pi / 64.0)))
		}
	}
	return
}()

//nolint:gosec // fixed-size arrays; every index is provably in range
func matrixSynthesis(s *[32]float32) (v [64]float32) {
	for i := range 64 {
		sum := float32(0)
		for j := range 32 {
			sum += synthNWin[i][j] * s[j]
		}
		v[i] = sum
	}
	return
}

//nolint:gosec // fixed-size arrays and a seeded PRNG; not cryptography
func TestSynthesisMatrixAgainstOracle(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	inputs := make([][32]float32, 0, 100)
	// Edge cases: silence, single impulses, extremes, alternating signs.
	inputs = append(inputs, [32]float32{})
	for k := range 32 {
		var in [32]float32
		in[k] = 1
		inputs = append(inputs, in)
	}
	var alt, big [32]float32
	for k := range 32 {
		if k%2 == 0 {
			alt[k] = 1
		} else {
			alt[k] = -1
		}
		big[k] = 1e6
	}
	inputs = append(inputs, alt, big)
	// Random inputs across the range real subband samples take.
	for range 64 {
		var in [32]float32
		for k := range 32 {
			in[k] = float32(r.NormFloat64())
		}
		inputs = append(inputs, in)
	}

	for n, in := range inputs {
		want := matrixSynthesis(&in)

		var v [1024]float32
		synthesisMatrix(&in, &v)

		var scale float32
		for _, w := range want {
			if abs := float32(math.Abs(float64(w))); abs > scale {
				scale = abs
			}
		}
		if scale < 1 {
			scale = 1
		}
		const tol = 1e-5

		for i := range 64 {
			if diff := math.Abs(float64(v[i] - want[i])); diff > float64(scale*tol) {
				t.Fatalf("input %d: v[%d] = %v, oracle = %v (diff %g, tolerance %g)",
					n, i, v[i], want[i], diff, float64(scale*tol))
			}
		}
		for i := 64; i < 1024; i++ {
			if v[i] != 0 {
				t.Fatalf("input %d: synthesisMatrix wrote past the first 64 entries at %d", n, i)
			}
		}
	}
}

//nolint:gosec // fixed-size arrays and a seeded PRNG; not cryptography
func BenchmarkSynthesisMatrix(b *testing.B) {
	var in [32]float32
	r := rand.New(rand.NewSource(1))
	for k := range 32 {
		in[k] = float32(r.NormFloat64())
	}
	var v [1024]float32

	for b.Loop() {
		synthesisMatrix(&in, &v)
	}
}
