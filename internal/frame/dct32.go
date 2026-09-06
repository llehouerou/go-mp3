package frame

import "math"

// dctCos[level][i] = 1 / (2*cos((2i+1)*pi/(2n))) for block size n = 32>>level.
// These are the Lee recursion's odd-part scaling factors, one table per level.
var dctCos [5][16]float32

func init() {
	for level := range 5 {
		n := 32 >> level
		for i := range n / 2 {
			dctCos[level][i] = float32(1 / (2 * math.Cos(float64(2*i+1)*math.Pi/float64(2*n))))
		}
	}
}

// dct32 computes the unnormalised 32-point DCT-II:
//
//	out[k] = sum(j<32) in[j] * cos(k*(2j+1)*pi/64)
//
// It uses the Lee recursion, flattened into five forward butterfly passes and
// five combine passes over two scratch buffers, instead of the 32 multiply-adds
// per output a direct evaluation would need.
//
// Lee's decomposition of a size-n DCT-II into two of size n/2:
//
//	g[i] = x[i] + x[n-1-i]                                -> X[2k]   = G[k]
//	h[i] = (x[i] - x[n-1-i]) / (2*cos((2i+1)*pi/(2n)))    -> X[2k+1] = H[k] + H[k+1]
//
// with H[n/2] = 0.
func dct32(in, out *[32]float32) {
	a := *in
	var b [32]float32
	src, dst := &a, &b

	for level := range 5 {
		n := 32 >> level
		half := n / 2
		for base := 0; base < 32; base += n {
			for i := range half {
				x, y := src[base+i], src[base+n-1-i]
				dst[base+i] = x + y
				dst[base+half+i] = (x - y) * dctCos[level][i]
			}
		}
		src, dst = dst, src
	}

	// Each element now holds a size-1 transform; combine back up the levels.
	for level := 4; level >= 0; level-- {
		n := 32 >> level
		half := n / 2
		for base := 0; base < 32; base += n {
			for k := range half {
				dst[base+2*k] = src[base+k]
				odd := src[base+half+k]
				if k+1 < half {
					odd += src[base+half+k+1]
				}
				dst[base+2*k+1] = odd
			}
		}
		src, dst = dst, src
	}

	*out = *src
}

// synthesisMatrix fills the 64 vVec entries of the polyphase synthesis from the
// 32-point DCT-II of the subband samples. The ISO matrix
// n[i][j] = cos((16+i)*(2j+1)*pi/64) is the DCT-II continued past k=31: with
// m = 16+i it is even in m, has period 128, and satisfies n(64-m) = -n(m), so
// every row is +/- one DCT coefficient.
//
//nolint:gosec // fixed-size arrays; every index below is provably in range
func synthesisMatrix(s *[32]float32, v *[1024]float32) {
	var h [32]float32
	dct32(s, &h)

	for i := range 16 {
		v[i] = h[16+i] // m = 16..31
	}
	v[16] = 0 // m = 32: n(32) = -n(32)
	for i := 17; i < 48; i++ {
		v[i] = -h[48-i] // m = 33..63: n(m) = -n(64-m)
	}
	for i := 48; i < 64; i++ {
		v[i] = -h[i-48] // m = 64..79: n(m) = -n(m-64)
	}
}
