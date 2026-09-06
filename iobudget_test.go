package mp3_test

import (
	"io"
	"testing"

	mp3 "github.com/llehouerou/go-mp3"
	"github.com/llehouerou/go-mp3/internal/testaudio"
)

// budgetFile is large enough that a full walk of it is unmistakable in the
// counters: 400 frames of 417 bytes, about 167 KB.
func budgetFile() testaudio.Options {
	return testaudio.Options{Frames: 400, XingMode: testaudio.Xing, LAME: true}
}

// TestOpenIOBudget pins the I/O cost of opening a file. The bug in #1 is
// invisible to every output assertion in this suite -- only these counters see
// it, which is why they are asserted rather than merely reported.
//
// Opening now reads the first block, peeks the Xing header in it, and asks the
// file how big it is. Nothing here may scale with the length of the file.
func TestOpenIOBudget(t *testing.T) {
	data := testaudio.Build(budgetFile())
	c := testaudio.NewCounter(data)

	if _, err := mp3.NewDecoderWithOptions(c, mp3.DecoderOptions{}); err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}

	t.Logf("open: %d reads, %d seeks, %d bytes read, file is %d bytes",
		c.Reads, c.Seeks, c.BytesRead, len(data))

	const maxCalls = 8
	if c.Reads > maxCalls {
		t.Errorf("open performed %d reads, want at most %d", c.Reads, maxCalls)
	}
	if c.Seeks > maxCalls {
		t.Errorf("open performed %d seeks, want at most %d", c.Seeks, maxCalls)
	}
	// One buffer's worth, give or take; certainly not the whole file.
	if budget := 2 * 64 * 1024; c.BytesRead > budget {
		t.Errorf("open read %d bytes of a %d-byte file, want at most %d",
			c.BytesRead, len(data), budget)
	}
	if c.MaxOffset > int64(len(data))/2 {
		t.Errorf("open read as far as offset %d of %d: it is walking the file again",
			c.MaxOffset, len(data))
	}
}

// TestPlaythroughIOBudget pins the total cost of open plus a straight
// play-through: a listener that never seeks must read the file about once.
func TestPlaythroughIOBudget(t *testing.T) {
	data := testaudio.Build(budgetFile())
	c := testaudio.NewCounter(data)

	d, err := mp3.NewDecoderWithOptions(c, mp3.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	if _, err := io.ReadAll(d); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	ratio := float64(c.BytesRead) / float64(len(data))
	t.Logf("open+playthrough: %d reads, %d seeks, %d bytes read for a %d-byte file (%.2fx)",
		c.Reads, c.Seeks, c.BytesRead, len(data), ratio)

	if ratio > 1.2 {
		t.Errorf("read %.2fx the file to play it once, want at most 1.2x "+
			"(nothing but the decode itself)", ratio)
	}
}

// TestSeekIOBudget pins what seeking costs. The frame index is no longer built
// at open, so the first seek pays for it: one walk of the file, once, and then
// the tail that is actually played. Later seeks reuse the index.
func TestSeekIOBudget(t *testing.T) {
	data := testaudio.Build(budgetFile())
	c := testaudio.NewCounter(data)

	d, err := mp3.NewDecoderWithOptions(c, mp3.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	beforeSeek := c.BytesRead

	if err := d.SeekToSample(d.SampleCount() / 2); err != nil {
		t.Fatalf("SeekToSample: %v", err)
	}
	if _, err := io.ReadAll(d); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	seekCost := c.BytesRead - beforeSeek
	t.Logf("seek to midpoint + play to end: %d bytes read (open cost %d bytes)",
		seekCost, beforeSeek)

	// One walk to build the index, plus the half of the file being played.
	if budget := 2 * len(data); seekCost > budget {
		t.Errorf("seek to the midpoint plus playing the tail read %d bytes, "+
			"want at most %d", seekCost, budget)
	}
}

// TestSecondSeekIsFree pins that the index survives: seeking again must not
// walk the file a second time.
func TestSecondSeekIsFree(t *testing.T) {
	data := testaudio.Build(budgetFile())
	c := testaudio.NewCounter(data)

	d, err := mp3.NewDecoderWithOptions(c, mp3.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}
	if err := d.SeekToSample(d.SampleCount() / 2); err != nil {
		t.Fatalf("SeekToSample: %v", err)
	}

	beforeSecond := c.BytesRead
	if err := d.SeekToSample(d.SampleCount() / 4); err != nil {
		t.Fatalf("SeekToSample: %v", err)
	}
	secondSeek := c.BytesRead - beforeSecond
	t.Logf("second seek: %d bytes read", secondSeek)

	if budget := 2 * 64 * 1024; secondSeek > budget {
		t.Errorf("second seek read %d bytes, want at most %d: the frame index "+
			"was rebuilt", secondSeek, budget)
	}
}
