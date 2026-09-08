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

package imdct

import (
	"math"
)

var imdctWinData = [4][36]float32{}

func init() {
	for i := range 36 {
		imdctWinData[0][i] = float32(math.Sin(math.Pi / 36 * (float64(i) + 0.5)))
	}
	for i := range 18 {
		imdctWinData[1][i] = float32(math.Sin(math.Pi / 36 * (float64(i) + 0.5)))
	}
	for i := 18; i < 24; i++ {
		imdctWinData[1][i] = 1.0
	}
	for i := 24; i < 30; i++ {
		imdctWinData[1][i] = float32(math.Sin(math.Pi / 12 * (float64(i) + 0.5 - 18.0)))
	}
	for i := 30; i < 36; i++ {
		imdctWinData[1][i] = 0.0
	}
	for i := range 12 {
		imdctWinData[2][i] = float32(math.Sin(math.Pi / 12 * (float64(i) + 0.5)))
	}
	for i := 12; i < 36; i++ {
		imdctWinData[2][i] = 0.0
	}
	for i := range 6 {
		imdctWinData[3][i] = 0.0
	}
	for i := 6; i < 12; i++ {
		imdctWinData[3][i] = float32(math.Sin(math.Pi / 12 * (float64(i) + 0.5 - 6.0)))
	}
	for i := 12; i < 18; i++ {
		imdctWinData[3][i] = 1.0
	}
	for i := 18; i < 36; i++ {
		imdctWinData[3][i] = float32(math.Sin(math.Pi / 36 * (float64(i) + 0.5)))
	}
}

// cosN12 is the short-block kernel stored output-major, cos[p][m], so each
// output's dot product walks contiguous memory.
var cosN12 = [12][6]float32{}

func init() {
	const N12 = 12
	for p := range 12 {
		for m := range 6 {
			cosN12[p][m] = float32(math.Cos(math.Pi / (2 * N12) * (2*float64(p) + 1 + N12/2) * (2*float64(m) + 1)))
		}
	}
}

// Twiddles of the 18-point DCT-IV computed through a 9-point complex FFT.
//
//	X[k] = sum(m<18) in[m] * cos(pi/18 * (k+1/2) * (m+1/2))
//
// Splitting m into even indices 2j and odd ones 17-2j and pairing them as
// z[j] = in[2j] + i*in[17-2j] turns the cosine kernel into
//
//	X[2k]    =  Re(Y[k])
//	X[17-2k] = -Im(Y[k])
//	Y[k]     = e^(-i*pi*(4k+1)/72) * FFT9(z[j] * e^(-i*pi*j/18))[k]
//
// FFT9 is radix-3 by radix-3, so the whole transform is about 110 multiplies
// against 324 for the dot products it replaces.
// cplx is a complex number in float32. Go's complex64 is not one the compiler
// optimises: the same transform on it measured twice as slow.
type cplx struct{ re, im float32 }

func (a cplx) mul(b cplx) cplx { return cplx{a.re*b.re - a.im*b.im, a.re*b.im + a.im*b.re} }

// unit is e^(-i*theta).
func unit(theta float64) cplx {
	return cplx{float32(math.Cos(theta)), float32(-math.Sin(theta))}
}

var (
	preTw  [9]cplx // e^(-i*pi*j/18)
	postTw [9]cplx // e^(-i*pi*(4k+1)/72)
	w9     [3]cplx // e^(-2*i*pi*n/9) for n = 1, 2, 4
)

func init() {
	for j := range 9 {
		preTw[j] = unit(math.Pi * float64(j) / 18)
		postTw[j] = unit(math.Pi * float64(4*j+1) / 72)
	}
	for i, n := range []float64{1, 2, 4} {
		w9[i] = unit(2 * math.Pi * n / 9)
	}
}

// sin60 is the radix-3 butterfly constant sqrt(3)/2.
const sin60 = 0.86602540378443864676

// fft3 is the radix-3 butterfly for W3 = e^(-2*i*pi/3), on scalars rather
// than cplx: the struct version costs 87 inliner points against a budget of
// 80, and not inlining it makes Win a third slower.
//
//nolint:gocritic // six results is the price of inlining, see above
func fft3(ar, ai, br, bi, cr, ci float32) (x0r, x0i, x1r, x1i, x2r, x2i float32) {
	tr, ti := br+cr, bi+ci
	dr, di := (br-cr)*sin60, (bi-ci)*sin60 // -i*d rotates to (di, -dr)
	mr, mi := ar-tr/2, ai-ti/2
	return ar + tr, ai + ti, mr + di, mi - dr, mr - di, mi + dr
}

// dct4x18 computes the 18-point DCT-IV of in, see the twiddle tables above.
//
//nolint:gosec // fixed-size arrays; every index below is provably in range
func dct4x18(x, in *[18]float32) {
	var z [9]cplx
	z[0] = cplx{in[0], in[17]}
	for j := 1; j < 9; j++ {
		z[j] = cplx{in[2*j], in[17-2*j]}.mul(preTw[j])
	}

	// FFT9 as radix-3 by radix-3: three FFT3s over stride-3 inputs, twiddle
	// by W9^(j2*k1), three FFT3s across.
	var u [9]cplx // u[j2*3 + k1]
	for j2 := range 3 {
		a, b, c := z[j2], z[j2+3], z[j2+6]
		u[j2*3].re, u[j2*3].im, u[j2*3+1].re, u[j2*3+1].im, u[j2*3+2].re, u[j2*3+2].im =
			fft3(a.re, a.im, b.re, b.im, c.re, c.im)
	}
	u[4] = u[4].mul(w9[0]) // W9^1
	u[5] = u[5].mul(w9[1]) // W9^2
	u[7] = u[7].mul(w9[1]) // W9^2
	u[8] = u[8].mul(w9[2]) // W9^4
	for k1 := range 3 {
		a, b, c := u[k1], u[3+k1], u[6+k1]
		z[k1].re, z[k1].im, z[k1+3].re, z[k1+3].im, z[k1+6].re, z[k1+6].im =
			fft3(a.re, a.im, b.re, b.im, c.re, c.im)
	}

	for k := range 9 {
		y := z[k].mul(postTw[k])
		x[2*k] = y.re
		x[17-2*k] = -y.im
	}
}

// Win performs the inverse modified DCT and windowing, writing all 36 outputs.
//
// The IMDCT's 2n outputs hold only n distinct values. With
//
//	x[p] = sum(m<n) in[m] * cos(pi/(2N) * (2p+1+N/2) * (2m+1)), N = 2n
//
// shifting p by 2N flips the sign, and reflecting p about -(1+n)/2 leaves x
// unchanged; together those give x[p] = -x[n-1-p] and x[n+k] = x[N-1-k]. So
// half the outputs are a copy with a sign, and only the windowing has to visit
// every output.
//
// For long blocks the n distinct values are the n-point DCT-IV X: writing
// 2p+1+N/2 as 2(p+n/2)+1 gives x[p] = X[p+n/2], and the same reflections on X
// give X[2n-1-k] = -X[k], so x[n+k] = -X[n/2-1-k].
//
//nolint:gosec // fixed-size arrays; every index below is provably in range
func Win(out *[36]float32, in *[18]float32, blockType int) {
	iwd := &imdctWinData[blockType]

	if blockType == 2 {
		clear(out[:]) // Short blocks write out[6:30] only
		for i := range 3 {
			base := 6*i + 6
			for p := range 3 {
				sum := float32(0.0)
				w := &cosN12[p]
				for m := range 6 {
					sum += in[i+3*m] * w[m]
				}
				out[base+p] += sum * iwd[p]
				out[base+5-p] += -sum * iwd[5-p]
			}
			for k := range 3 {
				sum := float32(0.0)
				w := &cosN12[6+k]
				for m := range 6 {
					sum += in[i+3*m] * w[m]
				}
				out[base+6+k] += sum * iwd[6+k]
				out[base+11-k] += sum * iwd[11-k]
			}
		}
		return
	}

	var x [18]float32
	dct4x18(&x, in)
	for p := range 9 {
		out[p] = x[9+p] * iwd[p]
		out[17-p] = -x[9+p] * iwd[17-p]
		out[18+p] = -x[8-p] * iwd[18+p]
		out[35-p] = -x[8-p] * iwd[35-p]
	}
}
