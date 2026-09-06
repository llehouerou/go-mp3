package imdct

import (
	"math"
	"math/rand"
	"testing"
)

// oracleCos is the IMDCT kernel built independently of the tables Win uses, so
// this test checks the symmetry reduction and the table layout at once.
func oracleCos(n, p, m int) float32 {
	return float32(math.Cos(math.Pi / (2 * float64(n)) * (2*float64(p) + 1 + float64(n)/2) * (2*float64(m) + 1)))
}

// winMatrix is the IMDCT written out longhand, evaluating the full cosine
// matrix for every output. It is what Win used to do, kept here as the oracle
// the symmetry-reduced version must reproduce.
//
//nolint:gosec // fixed-size arrays; every index is provably in range
func winMatrix(out *[36]float32, in *[18]float32, blockType int) {
	clear(out[:])
	iwd := imdctWinData[blockType]
	if blockType == 2 {
		const N = 12
		for i := range 3 {
			for p := range N {
				sum := float32(0.0)
				for m := range N / 2 {
					sum += in[i+3*m] * oracleCos(N, p, m)
				}
				out[6*i+p+6] += sum * iwd[p]
			}
		}
		return
	}
	const N = 36
	for p := range N {
		sum := float32(0.0)
		for m := range N / 2 {
			sum += in[m] * oracleCos(N, p, m)
		}
		out[p] = sum * iwd[p]
	}
}

//nolint:gosec // fixed-size arrays and a seeded PRNG; not cryptography
func TestWinAgainstOracle(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	inputs := make([][18]float32, 0, 100)
	inputs = append(inputs, [18]float32{})
	for k := range 18 {
		var in [18]float32
		in[k] = 1
		inputs = append(inputs, in)
	}
	var alt, big [18]float32
	for k := range 18 {
		if k%2 == 0 {
			alt[k] = 1
		} else {
			alt[k] = -1
		}
		big[k] = 1e6
	}
	inputs = append(inputs, alt, big)
	for range 64 {
		var in [18]float32
		for k := range 18 {
			in[k] = float32(r.NormFloat64())
		}
		inputs = append(inputs, in)
	}

	for blockType := range 4 {
		for n, in := range inputs {
			var want, got [36]float32
			winMatrix(&want, &in, blockType)
			Win(&got, &in, blockType)

			var scale float32 = 1
			for _, w := range want {
				if a := float32(math.Abs(float64(w))); a > scale {
					scale = a
				}
			}
			const tol = 1e-5

			for i := range 36 {
				if diff := math.Abs(float64(got[i] - want[i])); diff > float64(scale*tol) {
					t.Fatalf("blockType %d, input %d: out[%d] = %v, oracle = %v (diff %g, tolerance %g)",
						blockType, n, i, got[i], want[i], diff, float64(scale*tol))
				}
			}
		}
	}
}

//nolint:gosec // fixed-size arrays and a seeded PRNG; not cryptography
func BenchmarkWin(b *testing.B) {
	var in [18]float32
	r := rand.New(rand.NewSource(1))
	for k := range 18 {
		in[k] = float32(r.NormFloat64())
	}
	var out [36]float32

	for _, bt := range []int{0, 2} {
		b.Run([]string{"long", "", "short"}[bt], func(b *testing.B) {
			for b.Loop() {
				Win(&out, &in, bt)
			}
		})
	}
}
