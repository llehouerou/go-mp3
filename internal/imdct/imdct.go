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

var cosN12 = [6][12]float32{}

func init() {
	const N = 12
	for i := range 6 {
		for j := range 12 {
			cosN12[i][j] = float32(math.Cos(math.Pi / (2 * N) * (2*float64(j) + 1 + N/2) * (2*float64(i) + 1)))
		}
	}
}

var cosN36 = [18][36]float32{}

func init() {
	const N = 36
	for i := range 18 {
		for j := range 36 {
			cosN36[i][j] = float32(math.Cos(math.Pi / (2 * N) * (2*float64(j) + 1 + N/2) * (2*float64(i) + 1)))
		}
	}
}

func Win(in []float32, blockType int) []float32 {
	out := make([]float32, 36)
	if blockType == 2 {
		iwd := imdctWinData[blockType]
		const N = 12
		for i := range 3 {
			for p := range N {
				sum := float32(0.0)
				for m := range N / 2 {
					sum += in[i+3*m] * cosN12[m][p]
				}
				out[6*i+p+6] += sum * iwd[p] //nolint:gosec // p < 12 and iwd is [36]float32
			}
		}
		return out
	}
	const N = 36
	iwd := imdctWinData[blockType]
	for p := range N {
		sum := float32(0.0)
		for m := range N / 2 {
			sum += in[m] * cosN36[m][p]
		}
		out[p] = sum * iwd[p] //nolint:gosec // p < 36 and iwd is [36]float32
	}
	return out
}
