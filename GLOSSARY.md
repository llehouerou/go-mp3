# go-mp3

A pure-Go MP3 decoder. It turns an `io.Reader` of MP3 bytes into a stream of
16-bit stereo PCM, and answers questions about position and total length while
doing so.

## Language

**Frame**:
The unit an MP3 file is built from: a header plus its compressed audio, always
decoding to the same number of PCM bytes for a given sample rate.
_Avoid_: block, chunk, packet

**Granule**:
The unit the audio inside a frame is coded in: 576 frequency lines per
channel, two per frame in MPEG-1 and one in MPEG-2. `granule.Reader` turns a
frame's bitstream into finished granules; the DSP stages in `frame` consume
them one at a time.
_Avoid_: sub-frame, half-frame

**Bit reservoir**:
The bytes of earlier frames a frame's main data may begin in, pointed to by
`main_data_begin`. It is the only decoder state that a frame depends on
besides the synthesis history, and the reason a seek pre-rolls one frame.
_Avoid_: back-buffer, main data buffer

**Frame index**:
The mapping from frame number to byte offset in the source, held in
`frameStarts`. Needed only to seek; playback never consults it.
_Avoid_: frame table, offsets array, TOC (TOC means the Xing table specifically)

**Up-front scan**:
Walking every frame header of the file at open time to count frames and build
the frame index. The historical way this decoder learned a file's length.
_Avoid_: prescan, indexing pass

**Xing-derived length**:
A total length computed from the frame count in a Xing/Info header instead of
by counting frames. Costs no extra I/O because the header sits in the first
frame, which is read anyway.
_Avoid_: header length, VBR length

**Raw length**:
The total decoded PCM bytes a file yields, before any gapless trimming.
_Avoid_: real length, physical length

**Virtual length**:
The length a caller sees: raw length minus the encoder delay and padding
trimmed for gapless playback. `Length()`, `Duration()` and `Progress()` all
speak in virtual length.
_Avoid_: trimmed length, effective length, gapless length

**Length-known**:
Whether the decoder can state a total length. Distinct from seekability: a
non-seekable stream carrying a Xing header is length-known but not seekable.
_Avoid_: sized, measurable

**Seekable**:
Whether the underlying source implements `io.Seeker`. Independent of
length-known; `Seek` fails with `ErrNotSeekable` when it is false.
_Avoid_: random access, indexable

**Gapless trimming**:
Discarding the encoder delay at the start and the padding at the end, using
LAME/Xing metadata, so consecutive tracks join without a gap.
_Avoid_: silence stripping, delay compensation

**Reference decoder**:
An external decoder whose output this one is measured against. mpg123 in
practice; it defines what "correct" PCM means here, since no bit-exact
specification output is on hand.
_Avoid_: golden output, ground truth

**Compliance level**:
How close output sits to the reference, in ISO/IEC 11172-4 terms: _full_
compliance and _limited_ compliance are fixed RMS and maximum-difference
thresholds. This decoder meets limited compliance.
_Avoid_: accuracy level, quality level

**Accuracy budget**:
The deviation from the reference decoder this project allows itself, pinned
near what it currently measures rather than at the looser ISO limit. It is the
output contract: PCM is bounded, not identical bit-for-bit across versions.
_Avoid_: tolerance, error margin
