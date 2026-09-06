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

// TestOpenIOBudget records the I/O cost of opening a file. The bug in #1 is
// invisible to every output assertion in this suite -- only these counters see
// it, which is why they are asserted rather than merely reported.
//
// Today the up-front scan walks the whole file, so the budget is deliberately
// slack: it pins the shape (one full walk, no more) rather than the goal.
// Deriving length from the Xing header tightens these numbers to a few reads
// of the first frame, and this test is where that tightening gets proved.
func TestOpenIOBudget(t *testing.T) {
	data := testaudio.Build(budgetFile())
	c := testaudio.NewCounter(data)

	if _, err := mp3.NewDecoderWithOptions(c, mp3.DecoderOptions{}); err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}

	t.Logf("open: %d reads, %d seeks, %d bytes read, file is %d bytes",
		c.Reads, c.Seeks, c.BytesRead, len(data))

	// The scan reads one header per frame and seeks over each body, so both
	// counters scale with the frame count. Anything materially above that
	// means a second walk crept in.
	const frames = 401 // 400 audio frames plus the Xing frame
	if c.Reads > 3*frames {
		t.Errorf("open performed %d reads for %d frames: more than one walk of the file",
			c.Reads, frames)
	}
	if c.Seeks > 2*frames {
		t.Errorf("open performed %d seeks for %d frames: more than one walk of the file",
			c.Seeks, frames)
	}
	if c.MaxOffset < int64(len(data))-4608 {
		t.Errorf("open only reached offset %d of %d: the file was not fully walked, "+
			"so this budget no longer describes what open does -- retune it",
			c.MaxOffset, len(data))
	}
}

// TestPlaythroughIOBudget pins the total cost of open plus a straight
// play-through. A pure listener should read the file about once; today it reads
// it about twice because the scan happens first.
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

	if ratio > 2.2 {
		t.Errorf("read %.2fx the file to play it once, want at most 2.2x "+
			"(one scan plus one decode)", ratio)
	}
}

// TestSeekIOBudget pins what a seek costs on top of a play-through. The frame
// index is free today because the scan already built it; once it is built
// lazily, this is the test that keeps a single seek from costing more than one
// extra walk.
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

	// Playing the second half is half the file; the seek itself must not cost
	// another full walk on top of that.
	if budget := len(data); seekCost > budget {
		t.Errorf("seek to the midpoint plus playing the tail read %d bytes, "+
			"want at most %d", seekCost, budget)
	}
}
