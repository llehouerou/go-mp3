# Accuracy budget over bit-exact output

Decoded PCM is bounded by an accuracy budget measured against a reference
decoder, not promised to be identical bit-for-bit from one version to the next.
The polyphase synthesis was a 64x32 matrix multiply written out longhand, worth
46% of decode time (issue #8); replacing it with a fast DCT reassociates float
additions, so the last bits of the output move. That trade — inaudible rounding
differences for a 1.4x faster decoder — is only available if bit-stability was
never the promise.

It never was. The decoder already sat at ISO/IEC 11172-4 *limited* compliance
against mpg123 (RMS ~0.74 against a full-compliance threshold of 0.289, MaxDiff
1-2 LSB), and no test asserted byte-equality against an external reference. This
ADR writes down the contract that was already in force, and pins it.

## Considered Options

- **Promising bit-exact output.** Rejected. It would freeze current output with
  golden hashes, permanently rule out the fast DCT (leaving only the ~3%
  available from removing the vVec history copy), and equally freeze any future
  accuracy *improvement*, since raising precision also changes bits.
- **Leaving the existing ISO limited ceiling as the only gate** (RMS < 4.62,
  MaxDiff <= 32). Rejected: it is ~6x looser than the decoder's real deviation,
  so a wrong butterfly or a mis-signed twiddle in the DCT could pass it.
- **Committing reference PCM to testdata** so the accuracy gate runs without
  mpg123 installed. Rejected: a binary blob pinned to one mpg123 version's
  gapless behaviour, for a check that is a backstop rather than the primary
  guard.

## Consequences

- `compliance_test.go` pins RMS < 1.0 and MaxDiff <= 2, just above the measured
  0.72-0.75 / 1-2. A change that moves the output more than float reassociation
  does now fails, instead of disappearing into ISO's headroom.
- The primary guard for synthesis arithmetic is not the reference comparison,
  which skips when mpg123 is absent, but `internal/frame/dct32_test.go`: the ISO
  matrix `cos((16+i)(2j+1)pi/64)` lives there as an exact oracle and the fast DCT
  must reproduce it to ~1e-5 relative. It runs everywhere, in milliseconds.
- Downstream users cannot golden-hash decoder output across upgrades. README
  states this.
- The project itself does keep one golden hash, `golden_test.go`, over a
  synthesised file carrying real LAME frames (`internal/testaudio/testdata`).
  It is a tripwire, not a contract: it covers requantize/reorder/stereo/
  antialias on every machine, which the mpg123 comparison cannot, and a change
  that legitimately moves the last bits re-pins it. The mpg123 compliance test
  runs on the same synthesised file, so re-pinning is a measured decision.
- Full ISO compliance (RMS < 0.289) remains unreached and out of scope here; the
  residual error predates this change and comes from float32 precision through
  the whole pipeline, not from the synthesis matrixing.
