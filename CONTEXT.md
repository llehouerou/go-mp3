# go-mp3

A pure-Go MP3 decoder. It turns an `io.Reader` of MP3 bytes into a stream of
16-bit stereo PCM, and answers questions about position and total length while
doing so.

## Language

**Frame**:
The unit an MP3 file is built from: a header plus its compressed audio, always
decoding to the same number of PCM bytes for a given sample rate.
_Avoid_: block, chunk, packet

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
