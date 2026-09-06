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

// The cosine matrices are stored output-major, cos[p][m], so each output's dot
// product walks contiguous memory.
var (
	cosN12 = [12][6]float32{}
	cosN36 = [36][18]float32{}
)

func init() {
	const N12 = 12
	for p := range 12 {
		for m := range 6 {
			cosN12[p][m] = float32(math.Cos(math.Pi / (2 * N12) * (2*float64(p) + 1 + N12/2) * (2*float64(m) + 1)))
		}
	}
	const N36 = 36
	for p := range 36 {
		for m := range 18 {
			cosN36[p][m] = float32(math.Cos(math.Pi / (2 * N36) * (2*float64(p) + 1 + N36/2) * (2*float64(m) + 1)))
		}
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
// half the dot products are a copy with a sign, and only the windowing has to
// visit every output.
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

	for p := range 9 {
		sum := float32(0.0)
		w := &cosN36[p]
		for m := range in {
			sum += in[m] * w[m]
		}
		out[p] = sum * iwd[p]
		out[17-p] = -sum * iwd[17-p]
	}
	for k := range 9 {
		sum := float32(0.0)
		w := &cosN36[18+k]
		for m := range in {
			sum += in[m] * w[m]
		}
		out[18+k] = sum * iwd[18+k]
		out[35-k] = sum * iwd[35-k]
	}
}
