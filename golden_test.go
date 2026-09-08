package mp3_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/llehouerou/go-mp3/internal/testaudio"
)

// TestDecodeGolden is the always-on tripwire for the stages between side-info
// parsing and synthesis, which the mpg123 comparison covers only where mpg123
// is installed. It is a tripwire, not a contract (ADR-0002): a change that
// legitimately moves the last bits re-pins the hash, and the mpg123 compliance
// test on the same synthesised file is the arbiter of whether it was legitimate.
func TestDecodeGolden(t *testing.T) {
	d := open(t, testaudio.Build(testaudio.Options{Frames: 64, RealAudio: true}))
	pcm, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if want := 64 * 4608; len(pcm) != want {
		t.Fatalf("decoded %d bytes, want %d", len(pcm), want)
	}
	sum := sha256.Sum256(pcm)
	const want = "a34f5bcc7c67a2614dfbdcc63463de4decc86b6e97b622a5daeae2292d00cc81"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("decoded PCM sha256 = %s, want %s", got, want)
	}
}
