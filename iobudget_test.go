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
// The up-front scan still walks the whole file, but buffering means it does so
// in a handful of syscalls instead of one per frame, which is what hurt on a
// network filesystem. The budget below is therefore a constant, not a multiple
// of the frame count: no I/O at open may scale with the length of the file.
func TestOpenIOBudget(t *testing.T) {
	data := testaudio.Build(budgetFile())
	c := testaudio.NewCounter(data)

	if _, err := mp3.NewDecoderWithOptions(c, mp3.DecoderOptions{}); err != nil {
		t.Fatalf("NewDecoderWithOptions: %v", err)
	}

	t.Logf("open: %d reads, %d seeks, %d bytes read, file is %d bytes",
		c.Reads, c.Seeks, c.BytesRead, len(data))

	// 167 KB read in 64 KB blocks. Anything that scales with the 401 frames
	// would be orders of magnitude above this.
	const maxCalls = 32
	if c.Reads > maxCalls {
		t.Errorf("open performed %d reads, want at most %d: I/O at open must not "+
			"scale with the frame count", c.Reads, maxCalls)
	}
	if c.Seeks > maxCalls {
		t.Errorf("open performed %d seeks, want at most %d: I/O at open must not "+
			"scale with the frame count", c.Seeks, maxCalls)
	}
	if c.MaxOffset < int64(len(data))-4608 {
		t.Errorf("open only reached offset %d of %d: the file was not fully walked, "+
			"so this budget no longer describes what open does -- retune it",
			c.MaxOffset, len(data))
	}
}

// TestPlaythroughIOBudget pins the total cost of open plus a straight
// play-through. A pure listener should read the file about once; today it reads
// it about 2.4x because the scan runs first and buffering rounds every skipped
// frame body up to a block. That is the trade the issue predicted: fewer
// syscalls, more bytes. Removing the scan is what brings the volume down.
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

	if ratio > 2.5 {
		t.Errorf("read %.2fx the file to play it once, want at most 2.5x "+
			"(one buffered scan plus one decode)", ratio)
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
