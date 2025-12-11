// Copyright 2017 The go-mp3 Authors
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

package mp3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"testing"
)

// ISO/IEC 11172-4 compliance thresholds (relative to full scale ±1.0)
// For 16-bit audio, full scale is 32768
const (
	// Full compliance: RMS < 2^-15 / sqrt(12) ≈ 8.81e-6
	// In 16-bit samples: 8.81e-6 * 32768 ≈ 0.289
	fullComplianceRMS = 0.289

	// Limited compliance: RMS < 2^-11 / sqrt(12) ≈ 1.41e-4
	// In 16-bit samples: 1.41e-4 * 32768 ≈ 4.62
	limitedComplianceRMS = 4.62

	// Max absolute difference for full compliance: 2^-14 relative to full scale
	// In 16-bit samples: 2^-14 * 32768 = 2
	fullComplianceMaxDiff = 2

	// For limited compliance, max diff is typically 2^-10 * 32768 = 32
	limitedComplianceMaxDiff = 32
)

// ComplianceResult holds the results of comparing decoder output to reference
type ComplianceResult struct {
	File              string
	TotalSamples      int64
	RMS               float64
	MaxDiff           int16
	MaxDiffAt         int64
	MeanDiff          float64
	FullCompliance    bool
	LimitedCompliance bool
}

func (r ComplianceResult) String() string {
	status := "NOT COMPLIANT"
	if r.FullCompliance {
		status = "FULL COMPLIANCE"
	} else if r.LimitedCompliance {
		status = "LIMITED COMPLIANCE"
	}

	return fmt.Sprintf(`%s: %s
  Samples:  %d
  RMS:      %.6f (full < %.3f, limited < %.3f)
  MaxDiff:  %d at sample %d (full <= %d, limited <= %d)
  MeanDiff: %.6f`,
		r.File, status,
		r.TotalSamples,
		r.RMS, fullComplianceRMS, limitedComplianceRMS,
		r.MaxDiff, r.MaxDiffAt, fullComplianceMaxDiff, limitedComplianceMaxDiff,
		r.MeanDiff)
}

// decodeWithMpg123 decodes an MP3 file using mpg123 as reference decoder
func decodeWithMpg123(path string) ([]byte, error) {
	cmd := exec.Command("mpg123", "-e", "s16", "--stereo", "-s", path)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mpg123 failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// decodeWithGoMP3 decodes an MP3 file using this library
func decodeWithGoMP3(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(d)
}

// readSample reads a 16-bit signed sample from PCM data at the given byte offset
func readSample(data []byte, byteOffset int) int32 {
	return int32(int16(binary.LittleEndian.Uint16(data[byteOffset:]))) //nolint:gosec // intentional signed conversion
}

// computeRMSAtOffset computes RMS difference at a given offset using sampled comparison
func computeRMSAtOffset(reference, test []byte, offset, sampleStep int) float64 {
	refSamples := len(reference) / 4
	testSamples := len(test) / 4

	var refStart, testStart, compareLen int
	if offset >= 0 {
		refStart = 0
		testStart = offset
		compareLen = min(refSamples, testSamples-offset)
	} else {
		refStart = -offset
		testStart = 0
		compareLen = min(refSamples+offset, testSamples)
	}

	if compareLen <= 0 {
		return math.MaxFloat64
	}

	var sumSquaredDiff float64
	samplesCompared := 0
	for i := 0; i < compareLen; i += sampleStep {
		refIdx := (refStart + i) * 4
		testIdx := (testStart + i) * 4

		diffL := float64(readSample(test, testIdx) - readSample(reference, refIdx))
		sumSquaredDiff += diffL * diffL

		diffR := float64(readSample(test, testIdx+2) - readSample(reference, refIdx+2))
		sumSquaredDiff += diffR * diffR

		samplesCompared++
	}

	return math.Sqrt(sumSquaredDiff / float64(samplesCompared*2))
}

// findBestAlignment finds the sample offset that minimizes RMS difference
// between two PCM streams. This handles encoder delay differences between decoders.
// Returns the offset (positive means test is ahead of reference).
func findBestAlignment(reference, test []byte, maxOffset int) int {
	bestRMS := math.MaxFloat64
	bestOffset := 0

	// Phase 1: Coarse search with large steps, sparse sampling
	const coarseStep = 50        // Check every 50th offset
	const coarseSampleStep = 100 // Compare every 100th sample

	for offset := -maxOffset; offset <= maxOffset; offset += coarseStep {
		rms := computeRMSAtOffset(reference, test, offset, coarseSampleStep)
		if rms < bestRMS {
			bestRMS = rms
			bestOffset = offset
		}
	}

	// Phase 2: Fine search around best coarse result
	fineStart := max(-maxOffset, bestOffset-coarseStep)
	fineEnd := min(maxOffset, bestOffset+coarseStep)

	for offset := fineStart; offset <= fineEnd; offset++ {
		rms := computeRMSAtOffset(reference, test, offset, 10)
		if rms < bestRMS {
			bestRMS = rms
			bestOffset = offset
		}
	}

	return bestOffset
}

// compareDecoderOutputsWithOffset compares two PCM streams with a sample offset
func compareDecoderOutputsWithOffset(reference, test []byte, file string, offset int) ComplianceResult {
	result := ComplianceResult{
		File: file,
	}

	// Convert to stereo samples (4 bytes each)
	refSamples := len(reference) / 4
	testSamples := len(test) / 4

	var refStart, testStart, compareLen int

	if offset >= 0 {
		refStart = 0
		testStart = offset
		compareLen = min(refSamples, testSamples-offset)
	} else {
		refStart = -offset
		testStart = 0
		compareLen = min(refSamples+offset, testSamples)
	}

	if compareLen <= 0 {
		return result
	}

	result.TotalSamples = int64(compareLen * 2) // stereo = 2 samples per frame

	var sumSquaredDiff float64
	var sumDiff float64

	for i := range compareLen {
		refIdx := (refStart + i) * 4
		testIdx := (testStart + i) * 4

		// Left channel
		refL := readSample(reference, refIdx)
		testL := readSample(test, testIdx)
		diffL := testL - refL
		absDiffL := diffL
		if absDiffL < 0 {
			absDiffL = -absDiffL
		}
		if absDiffL > int32(result.MaxDiff) {
			result.MaxDiff = int16(absDiffL) //nolint:gosec // diff is bounded by int16 range
			result.MaxDiffAt = int64(i * 2)
		}
		sumSquaredDiff += float64(diffL) * float64(diffL)
		sumDiff += float64(diffL)

		// Right channel
		refR := readSample(reference, refIdx+2)
		testR := readSample(test, testIdx+2)
		diffR := testR - refR
		absDiffR := diffR
		if absDiffR < 0 {
			absDiffR = -absDiffR
		}
		if absDiffR > int32(result.MaxDiff) {
			result.MaxDiff = int16(absDiffR) //nolint:gosec // diff is bounded by int16 range
			result.MaxDiffAt = int64(i*2 + 1)
		}
		sumSquaredDiff += float64(diffR) * float64(diffR)
		sumDiff += float64(diffR)
	}

	if result.TotalSamples > 0 {
		result.RMS = math.Sqrt(sumSquaredDiff / float64(result.TotalSamples))
		result.MeanDiff = sumDiff / float64(result.TotalSamples)
	}

	// Check compliance levels
	result.FullCompliance = result.RMS < fullComplianceRMS && result.MaxDiff <= fullComplianceMaxDiff
	result.LimitedCompliance = result.RMS < limitedComplianceRMS && result.MaxDiff <= limitedComplianceMaxDiff

	return result
}

// TestComplianceAgainstMpg123 compares this decoder against mpg123
func TestComplianceAgainstMpg123(t *testing.T) {
	// Check if mpg123 is available
	if _, err := exec.LookPath("mpg123"); err != nil {
		t.Skip("mpg123 not found, skipping compliance test")
	}

	testFiles := []string{
		"example/classic.mp3",
		"example/classic_lame.mp3",
		"example/mpeg2.mp3",
	}

	for _, file := range testFiles {
		t.Run(file, func(t *testing.T) {
			if _, err := os.Stat(file); os.IsNotExist(err) {
				t.Skipf("test file not found: %s", file)
			}

			// Decode with mpg123 (reference)
			refPCM, err := decodeWithMpg123(file)
			if err != nil {
				t.Fatalf("mpg123 decode failed: %v", err)
			}

			// Decode with go-mp3
			testPCM, err := decodeWithGoMP3(file)
			if err != nil {
				t.Fatalf("go-mp3 decode failed: %v", err)
			}

			t.Logf("Reference length: %d bytes, go-mp3 length: %d bytes, diff: %d",
				len(refPCM), len(testPCM), len(testPCM)-len(refPCM))

			// If lengths differ significantly, find best alignment
			// (handles LAME gapless info that mpg123 respects but go-mp3 doesn't)
			offset := 0
			lenDiff := len(testPCM) - len(refPCM)
			if lenDiff != 0 {
				// Search for alignment within a reasonable range (up to 3000 stereo samples = ~68ms at 44.1kHz)
				// This covers typical LAME encoder delay (~1105 samples)
				maxSearch := 3000
				offset = findBestAlignment(refPCM, testPCM, maxSearch)
				t.Logf("Output lengths differ; found best alignment at offset %d stereo samples", offset)
			}

			// Compare with alignment
			result := compareDecoderOutputsWithOffset(refPCM, testPCM, file, offset)

			t.Logf("\n%s", result.String())

			// We aim for at least limited compliance
			if !result.LimitedCompliance {
				t.Errorf("decoder does not meet limited compliance requirements")
			}

			if result.FullCompliance {
				t.Logf("PASS: Full ISO/IEC 11172-4 compliance achieved!")
			} else if result.LimitedCompliance {
				t.Logf("PASS: Limited compliance achieved (acceptable for most applications)")
			}
		})
	}
}

// TestComplianceDetailedAnalysis provides more detailed analysis for debugging
func TestComplianceDetailedAnalysis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detailed analysis in short mode")
	}

	// Check if mpg123 is available
	if _, err := exec.LookPath("mpg123"); err != nil {
		t.Skip("mpg123 not found, skipping compliance test")
	}

	file := "example/classic_lame.mp3"
	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Skipf("test file not found: %s", file)
	}

	refPCM, err := decodeWithMpg123(file)
	if err != nil {
		t.Fatalf("mpg123 decode failed: %v", err)
	}

	testPCM, err := decodeWithGoMP3(file)
	if err != nil {
		t.Fatalf("go-mp3 decode failed: %v", err)
	}

	// Find best alignment first
	offset := findBestAlignment(refPCM, testPCM, 3000)
	t.Logf("Best alignment offset: %d stereo samples", offset)

	// Analyze difference distribution with alignment
	refSamples := len(refPCM) / 4
	testSamples := len(testPCM) / 4

	var refStart, testStart, compareLen int
	if offset >= 0 {
		refStart = 0
		testStart = offset
		compareLen = min(refSamples, testSamples-offset)
	} else {
		refStart = -offset
		testStart = 0
		compareLen = min(refSamples+offset, testSamples)
	}

	diffHist := make(map[int32]int)

	for i := range compareLen {
		refIdx := (refStart + i) * 4
		testIdx := (testStart + i) * 4

		// Left channel
		diffHist[readSample(testPCM, testIdx)-readSample(refPCM, refIdx)]++

		// Right channel
		diffHist[readSample(testPCM, testIdx+2)-readSample(refPCM, refIdx+2)]++
	}

	t.Logf("Difference distribution (top 10):")

	// Find most common differences
	type diffCount struct {
		diff  int32
		count int
	}
	diffs := make([]diffCount, 0, len(diffHist))
	for d, c := range diffHist {
		diffs = append(diffs, diffCount{d, c})
	}

	// Sort by count descending
	for i := range diffs {
		for j := i + 1; j < len(diffs); j++ {
			if diffs[j].count > diffs[i].count {
				diffs[i], diffs[j] = diffs[j], diffs[i]
			}
		}
	}

	for i := range min(10, len(diffs)) {
		t.Logf("  diff=%4d: %d samples (%.2f%%)",
			diffs[i].diff, diffs[i].count,
			100.0*float64(diffs[i].count)/float64(compareLen*2))
	}
}
