package mp3

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// nonSeekableReader wraps a reader and removes Seek capability
type nonSeekableReader struct {
	r io.Reader
}

func (n *nonSeekableReader) Read(p []byte) (int, error) {
	return n.r.Read(p)
}

// Tests for Duration()

func TestDuration_Seekable(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	if dur < 0 {
		t.Errorf("Duration() returned negative value: %v", dur)
	}

	// classic.mp3 should be around 355 seconds
	// Allow some tolerance for frame alignment
	if dur < 350*time.Second || dur > 360*time.Second {
		t.Errorf("Duration() = %v, expected around 355 seconds", dur)
	}
}

func TestDuration_NonSeekable(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	// Wrap in non-seekable reader
	nonSeekable := &nonSeekableReader{r: f}

	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	if dur != -1 {
		t.Errorf("Duration() for non-seekable = %v, expected -1", dur)
	}
}

func TestDuration_MPEG2(t *testing.T) {
	f, err := os.Open("example/mpeg2.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	if dur < 0 {
		t.Errorf("Duration() returned negative value: %v", dur)
	}

	// mpeg2.mp3 is about 75 seconds
	if dur < 70*time.Second || dur > 80*time.Second {
		t.Errorf("Duration() = %v, expected around 75 seconds", dur)
	}
}

// Tests for Position()

func TestPosition_Initial(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	pos := d.Position()
	if pos != 0 {
		t.Errorf("Position() at start = %v, expected 0", pos)
	}
}

func TestPosition_AfterRead(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Read 1 second worth of data at 44100Hz
	// 44100 samples * 4 bytes = 176400 bytes
	sampleRate := d.SampleRate()
	bytesToRead := sampleRate * 4
	buf := make([]byte, bytesToRead)
	n, err := io.ReadFull(d, buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if n != bytesToRead {
		t.Fatalf("read %d bytes, expected %d", n, bytesToRead)
	}

	pos := d.Position()
	expected := time.Second
	// Allow small tolerance due to rounding
	diff := pos - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after reading 1s = %v, expected %v", pos, expected)
	}
}

func TestPosition_AfterSeek(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to 10 seconds (byte position = 10 * sampleRate * 4)
	sampleRate := d.SampleRate()
	targetBytes := int64(10 * sampleRate * 4)
	_, err = d.Seek(targetBytes, io.SeekStart)
	if err != nil {
		t.Fatalf("failed to seek: %v", err)
	}

	pos := d.Position()
	expected := 10 * time.Second
	diff := pos - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after seek = %v, expected %v", pos, expected)
	}
}

func TestPosition_NonSeekable(t *testing.T) {
	// Even for non-seekable streams, Position() should track read progress
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Initial position should be 0
	if pos := d.Position(); pos != 0 {
		t.Errorf("Position() at start = %v, expected 0", pos)
	}

	// Read some data
	buf := make([]byte, d.SampleRate()*4) // 1 second
	if _, err := io.ReadFull(d, buf); err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	// Position should be approximately 1 second
	pos := d.Position()
	if pos < 900*time.Millisecond || pos > 1100*time.Millisecond {
		t.Errorf("Position() after 1s read = %v, expected ~1s", pos)
	}
}

// Tests for SeekToTime()

func TestSeekToTime_Start(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Read some data first
	buf := make([]byte, d.SampleRate()*4*5) // 5 seconds
	if _, err := io.ReadFull(d, buf); err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	// Seek back to start
	if err := d.SeekToTime(0); err != nil {
		t.Fatalf("SeekToTime(0) failed: %v", err)
	}

	if pos := d.Position(); pos != 0 {
		t.Errorf("Position() after SeekToTime(0) = %v, expected 0", pos)
	}
}

func TestSeekToTime_Middle(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	target := 30 * time.Second
	if err := d.SeekToTime(target); err != nil {
		t.Fatalf("SeekToTime(%v) failed: %v", target, err)
	}

	pos := d.Position()
	diff := pos - target
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after SeekToTime(%v) = %v", target, pos)
	}
}

func TestSeekToTime_End(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	if err := d.SeekToTime(dur); err != nil {
		t.Fatalf("SeekToTime(Duration) failed: %v", err)
	}

	pos := d.Position()
	diff := pos - dur
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after SeekToTime(Duration) = %v, expected %v", pos, dur)
	}
}

func TestSeekToTime_Negative(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Negative time should clamp to 0
	if err := d.SeekToTime(-5 * time.Second); err != nil {
		t.Fatalf("SeekToTime(-5s) failed: %v", err)
	}

	if pos := d.Position(); pos != 0 {
		t.Errorf("Position() after SeekToTime(-5s) = %v, expected 0", pos)
	}
}

func TestSeekToTime_BeyondEnd(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	// Seek beyond end should clamp to end
	if err := d.SeekToTime(dur + 100*time.Second); err != nil {
		t.Fatalf("SeekToTime(beyond end) failed: %v", err)
	}

	pos := d.Position()
	diff := pos - dur
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after SeekToTime(beyond end) = %v, expected %v", pos, dur)
	}
}

func TestSeekToTime_NonSeekable(t *testing.T) {
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seeking on non-seekable source should return error
	err = d.SeekToTime(10 * time.Second)
	if err == nil {
		t.Error("SeekToTime() on non-seekable source should return error")
	}
}

func TestSeekToTime_Alignment(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to a position that might not be 4-byte aligned
	target := 1500 * time.Millisecond // 1.5 seconds
	if err := d.SeekToTime(target); err != nil {
		t.Fatalf("SeekToTime(%v) failed: %v", target, err)
	}

	// Read some samples and verify we can read without issues
	buf := make([]byte, 1024)
	if _, err := d.Read(buf); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read after seek failed: %v", err)
	}
}

func TestSeekToTime_NoDurationMultiplicationBug(t *testing.T) {
	// This test specifically checks that time.Duration math is correct
	// The amanitaverna fork had a bug: time.Second * time.Duration(at)
	// which doubled the time value incorrectly
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	target := time.Second
	if err := d.SeekToTime(target); err != nil {
		t.Fatalf("SeekToTime(1s) failed: %v", err)
	}

	pos := d.Position()
	// Should be exactly 1 second, not 1 second squared
	diff := pos - target
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Duration multiplication bug detected: Position() = %v, want ~1s", pos)
	}
}

// Tests for Skip()

func TestSkip_Forward(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Skip forward 5 seconds
	if err := d.Skip(5 * time.Second); err != nil {
		t.Fatalf("Skip(5s) failed: %v", err)
	}

	pos := d.Position()
	expected := 5 * time.Second
	diff := pos - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after Skip(5s) = %v, expected %v", pos, expected)
	}
}

func TestSkip_Backward(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to 10 seconds
	if err := d.SeekToTime(10 * time.Second); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	// Skip backward 3 seconds
	if err := d.Skip(-3 * time.Second); err != nil {
		t.Fatalf("Skip(-3s) failed: %v", err)
	}

	pos := d.Position()
	expected := 7 * time.Second
	diff := pos - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after Skip(-3s) = %v, expected %v", pos, expected)
	}
}

func TestSkip_BeyondStart(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to 3 seconds
	if err := d.SeekToTime(3 * time.Second); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	// Skip back 10 seconds (should clamp to 0)
	if err := d.Skip(-10 * time.Second); err != nil {
		t.Fatalf("Skip(-10s) failed: %v", err)
	}

	if pos := d.Position(); pos != 0 {
		t.Errorf("Position() after Skip beyond start = %v, expected 0", pos)
	}
}

func TestSkip_BeyondEnd(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	// Seek to near end
	if err := d.SeekToTime(dur - 5*time.Second); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	// Skip forward 100 seconds (should clamp to end)
	if err := d.Skip(100 * time.Second); err != nil {
		t.Fatalf("Skip(100s) failed: %v", err)
	}

	pos := d.Position()
	diff := pos - dur
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after Skip beyond end = %v, expected %v", pos, dur)
	}
}

func TestSkip_NonSeekable(t *testing.T) {
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Skipping on non-seekable source should return error
	err = d.Skip(10 * time.Second)
	if err == nil {
		t.Error("Skip() on non-seekable source should return error")
	}
}

// Tests for Remaining()

func TestRemaining_Initial(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// At start, Remaining() == Duration()
	remaining := d.Remaining()
	dur := d.Duration()
	if remaining != dur {
		t.Errorf("Remaining() at start = %v, expected %v", remaining, dur)
	}
}

func TestRemaining_AfterSeek(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	seekTime := 10 * time.Second
	if err := d.SeekToTime(seekTime); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	remaining := d.Remaining()
	expected := d.Duration() - seekTime
	diff := remaining - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Remaining() after seek = %v, expected %v", remaining, expected)
	}
}

func TestRemaining_NonSeekable(t *testing.T) {
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	if remaining := d.Remaining(); remaining != -1 {
		t.Errorf("Remaining() for non-seekable = %v, expected -1", remaining)
	}
}

// Tests for Progress()

func TestProgress_Initial(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	progress := d.Progress()
	if progress != 0.0 {
		t.Errorf("Progress() at start = %v, expected 0.0", progress)
	}
}

func TestProgress_Middle(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	dur := d.Duration()
	if err := d.SeekToTime(dur / 2); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	progress := d.Progress()
	// Should be approximately 0.5
	if progress < 0.49 || progress > 0.51 {
		t.Errorf("Progress() at middle = %v, expected ~0.5", progress)
	}
}

func TestProgress_End(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	if err := d.SeekToTime(d.Duration()); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	progress := d.Progress()
	if progress < 0.99 || progress > 1.0 {
		t.Errorf("Progress() at end = %v, expected ~1.0", progress)
	}
}

func TestProgress_NonSeekable(t *testing.T) {
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	if progress := d.Progress(); progress != -1 {
		t.Errorf("Progress() for non-seekable = %v, expected -1", progress)
	}
}

// Tests for sample-based methods

func TestSamplePosition_Initial(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	if pos := d.SamplePosition(); pos != 0 {
		t.Errorf("SamplePosition() at start = %d, expected 0", pos)
	}
}

func TestSamplePosition_AfterRead(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Read 4 bytes = 1 sample (stereo 16-bit)
	buf := make([]byte, 4)
	if _, err := io.ReadFull(d, buf); err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	if pos := d.SamplePosition(); pos != 1 {
		t.Errorf("SamplePosition() after 1 sample = %d, expected 1", pos)
	}

	// Read 1 second worth (sampleRate samples)
	sampleRate := d.SampleRate()
	buf = make([]byte, sampleRate*4)
	if _, err := io.ReadFull(d, buf); err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	expectedSamples := int64(1 + sampleRate)
	if pos := d.SamplePosition(); pos != expectedSamples {
		t.Errorf("SamplePosition() = %d, expected %d", pos, expectedSamples)
	}
}

func TestSampleCount_Seekable(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	count := d.SampleCount()
	// Length / 4 (stereo 16-bit)
	expected := d.Length() / 4
	if count != expected {
		t.Errorf("SampleCount() = %d, expected %d", count, expected)
	}
}

func TestSampleCount_NonSeekable(t *testing.T) {
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	if count := d.SampleCount(); count != -1 {
		t.Errorf("SampleCount() for non-seekable = %d, expected -1", count)
	}
}

func TestSeekToSample_Valid(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to 1 second worth of samples
	sampleRate := d.SampleRate()
	if err := d.SeekToSample(int64(sampleRate)); err != nil {
		t.Fatalf("SeekToSample failed: %v", err)
	}

	// Position should be 1 second
	pos := d.Position()
	expected := time.Second
	diff := pos - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 10*time.Millisecond {
		t.Errorf("Position() after SeekToSample = %v, expected %v", pos, expected)
	}

	// SamplePosition should match
	if samplePos := d.SamplePosition(); samplePos != int64(sampleRate) {
		t.Errorf("SamplePosition() = %d, expected %d", samplePos, sampleRate)
	}
}

func TestSeekToSample_Clamping(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to negative sample (should clamp to 0)
	if err := d.SeekToSample(-100); err != nil {
		t.Fatalf("SeekToSample(-100) failed: %v", err)
	}
	if pos := d.SamplePosition(); pos != 0 {
		t.Errorf("SamplePosition() after negative seek = %d, expected 0", pos)
	}

	// Seek beyond end (should clamp)
	count := d.SampleCount()
	if err := d.SeekToSample(count + 10000); err != nil {
		t.Fatalf("SeekToSample(beyond end) failed: %v", err)
	}
	if pos := d.SamplePosition(); pos != count {
		t.Errorf("SamplePosition() after seek beyond end = %d, expected %d", pos, count)
	}
}

func TestSeekToSample_NonSeekable(t *testing.T) {
	data, err := os.ReadFile("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	nonSeekable := &nonSeekableReader{r: bytes.NewReader(data)}
	d, err := NewDecoder(nonSeekable)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	err = d.SeekToSample(1000)
	if err == nil {
		t.Error("SeekToSample() on non-seekable source should return error")
	}
}

// --- Phase 7: Integration Tests with Real MP3 Files ---

func TestIntegration_MPEG1_Classic(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Verify MPEG1 characteristics
	if d.SampleRate() != 44100 {
		t.Errorf("SampleRate() = %d, expected 44100 for MPEG1", d.SampleRate())
	}

	// Verify duration is reasonable (classic.mp3 is about 355 seconds)
	dur := d.Duration()
	if dur < 350*time.Second || dur > 360*time.Second {
		t.Errorf("Duration() = %v, expected ~355s", dur)
	}

	// Seek to middle and verify position
	midpoint := dur / 2
	if err := d.SeekToTime(midpoint); err != nil {
		t.Fatalf("SeekToTime(midpoint) failed: %v", err)
	}
	pos := d.Position()
	// Allow 30ms tolerance for frame alignment
	diff := pos - midpoint
	if diff < 0 {
		diff = -diff
	}
	if diff > 30*time.Millisecond {
		t.Errorf("Position() after seek = %v, expected ~%v", pos, midpoint)
	}

	// Read some audio data
	buf := make([]byte, 4096)
	n, err := d.Read(buf)
	if err != nil {
		t.Fatalf("Read() after seek failed: %v", err)
	}
	if n == 0 {
		t.Error("Read() returned 0 bytes after seek")
	}
}

func TestIntegration_MPEG2(t *testing.T) {
	f, err := os.Open("example/mpeg2.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// MPEG2 should have lower sample rate
	sr := d.SampleRate()
	if sr == 44100 || sr == 48000 || sr == 32000 {
		t.Errorf("SampleRate() = %d, expected MPEG2 rate (22050, 24000, or 16000)", sr)
	}

	// Verify duration is reasonable (mpeg2.mp3 is about 75 seconds based on plan)
	dur := d.Duration()
	if dur < 70*time.Second || dur > 80*time.Second {
		t.Errorf("Duration() = %v, expected ~75s", dur)
	}

	// Test seeking
	target := 30 * time.Second
	if err := d.SeekToTime(target); err != nil {
		t.Fatalf("SeekToTime(30s) failed: %v", err)
	}

	// Read some data
	buf := make([]byte, 4096)
	n, err := d.Read(buf)
	if err != nil {
		t.Fatalf("Read() after seek failed: %v", err)
	}
	if n == 0 {
		t.Error("Read() returned 0 bytes after seek")
	}
}

func TestIntegration_AudioIntegrity(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Seek to a specific position
	targetPos := 100 * time.Second
	if err := d.SeekToTime(targetPos); err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}

	// Read data at this position
	buf1 := make([]byte, 8192)
	n1, err := d.Read(buf1)
	if err != nil {
		t.Fatalf("first Read() failed: %v", err)
	}
	buf1 = buf1[:n1]
	pos1 := d.Position()

	// Seek away to a different position
	if err := d.SeekToTime(200 * time.Second); err != nil {
		t.Fatalf("SeekToTime(200s) failed: %v", err)
	}

	// Read some data (to exercise the decoder)
	discard := make([]byte, 4096)
	if _, err := d.Read(discard); err != nil {
		t.Fatalf("Read at 200s failed: %v", err)
	}

	// Seek back to original position
	if err := d.SeekToTime(targetPos); err != nil {
		t.Fatalf("SeekToTime back failed: %v", err)
	}

	// Verify position is same (within tolerance for frame alignment)
	pos2 := d.Position()
	posDiff := pos1 - pos2
	if posDiff < 0 {
		posDiff = -posDiff
	}
	if posDiff > 30*time.Millisecond {
		t.Errorf("Position after seek back = %v, expected ~%v", pos2, pos1)
	}

	// Read data again
	buf2 := make([]byte, 8192)
	n2, err := d.Read(buf2)
	if err != nil {
		t.Fatalf("second Read() failed: %v", err)
	}
	buf2 = buf2[:n2]

	// Compare the data - should be identical
	if n1 != n2 {
		t.Errorf("Read sizes differ: first=%d, second=%d", n1, n2)
	} else {
		for i := range n1 {
			if buf1[i] != buf2[i] {
				t.Errorf("Audio data differs at byte %d: first=%d, second=%d", i, buf1[i], buf2[i])
				break
			}
		}
	}
}

func TestIntegration_SeekToStart(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Read initial data
	buf1 := make([]byte, 4096)
	n1, err := d.Read(buf1)
	if err != nil {
		t.Fatalf("first Read() failed: %v", err)
	}
	buf1 = buf1[:n1]

	// Seek to middle
	if err := d.SeekToTime(d.Duration() / 2); err != nil {
		t.Fatalf("SeekToTime(middle) failed: %v", err)
	}

	// Read some data there
	discard := make([]byte, 4096)
	if _, err := d.Read(discard); err != nil {
		t.Fatalf("Read at middle failed: %v", err)
	}

	// Seek back to start
	if err := d.SeekToTime(0); err != nil {
		t.Fatalf("SeekToTime(0) failed: %v", err)
	}

	// Verify position is at start
	if d.Position() != 0 {
		t.Errorf("Position() after seek to 0 = %v, expected 0", d.Position())
	}

	// Read data again - should match initial data
	buf2 := make([]byte, 4096)
	n2, err := d.Read(buf2)
	if err != nil {
		t.Fatalf("second Read() failed: %v", err)
	}
	buf2 = buf2[:n2]

	// Compare
	if n1 != n2 {
		t.Errorf("Read sizes differ: first=%d, second=%d", n1, n2)
	} else {
		for i := range n1 {
			if buf1[i] != buf2[i] {
				t.Errorf("Audio data differs at byte %d after seek to start", i)
				break
			}
		}
	}
}

func TestIntegration_SkipForwardBackward(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// Start at 60 seconds
	if err := d.SeekToTime(60 * time.Second); err != nil {
		t.Fatalf("SeekToTime(60s) failed: %v", err)
	}
	pos1 := d.Position()

	// Skip forward 30 seconds
	if err := d.Skip(30 * time.Second); err != nil {
		t.Fatalf("Skip(30s) failed: %v", err)
	}
	pos2 := d.Position()

	// Verify we moved forward ~30 seconds
	diff := pos2 - pos1
	if diff < 29*time.Second || diff > 31*time.Second {
		t.Errorf("Skip(30s) moved %v, expected ~30s", diff)
	}

	// Skip backward 30 seconds
	if err := d.Skip(-30 * time.Second); err != nil {
		t.Fatalf("Skip(-30s) failed: %v", err)
	}
	pos3 := d.Position()

	// Verify we're back near original position
	diff = pos1 - pos3
	if diff < 0 {
		diff = -diff
	}
	if diff > 100*time.Millisecond {
		t.Errorf("Position after Skip forward and back = %v, expected ~%v", pos3, pos1)
	}
}

func TestIntegration_ProgressTracking(t *testing.T) {
	f, err := os.Open("example/classic.mp3")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	d, err := NewDecoder(f)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	// At start, progress should be ~0
	prog := d.Progress()
	if prog < 0 || prog > 0.01 {
		t.Errorf("Progress() at start = %f, expected ~0", prog)
	}

	// Seek to middle
	if err := d.SeekToTime(d.Duration() / 2); err != nil {
		t.Fatalf("SeekToTime(middle) failed: %v", err)
	}

	// Progress should be ~0.5
	prog = d.Progress()
	if prog < 0.45 || prog > 0.55 {
		t.Errorf("Progress() at middle = %f, expected ~0.5", prog)
	}

	// Seek to end
	if err := d.SeekToTime(d.Duration()); err != nil {
		t.Fatalf("SeekToTime(end) failed: %v", err)
	}

	// Progress should be ~1.0
	prog = d.Progress()
	if prog < 0.99 || prog > 1.01 {
		t.Errorf("Progress() at end = %f, expected ~1.0", prog)
	}
}
