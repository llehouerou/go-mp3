# Xing-derived length with scan fallback

The decoder used to learn a file's length with an up-front scan of every frame
header at open time, which reads the file twice and, on a network filesystem,
cost seconds of I/O stall per track (issue #1). Since a Xing/Info header
already carries the frame count and already gets parsed for gapless playback,
length is now derived from it, and the scan survives only as a fallback for
headerless files and as the reference implementation the fast path is tested
against.

## Considered Options

- **Deferring the scan to the first `Length()`/`Seek()` call.** Rejected as the
  primary fix: the main consumer asks for the length immediately after opening,
  so deferral moves the stall rather than removing it. It is still applied to
  the fallback path, where it costs nothing.
- **Xing TOC seeking.** Rejected: it would remove the frame index entirely but
  turns exact seeks into ~0.5%-accurate ones, and the bit-reservoir pre-roll on
  seek assumes a real frame boundary.
- **Buffering the source only.** Kept, but as a separate improvement: it removes
  the syscall amplification without removing the doubled read volume.

## Consequences

- A Xing frame count is trusted only after a cheap sanity check: on a seekable
  source, the file size must be at least the smallest size that frame count
  could occupy. A provably truncated file falls back to the scan.
- Length and seekability are no longer the same condition. A non-seekable
  stream with a Xing header reports a length; `Seek` returns `ErrNotSeekable`.
- The frame index is built on the first seek and cached, so playback-only
  callers never build it.
- The scan must stay reachable from tests: every fixture asserts that the
  Xing-derived length and post-seek byte stream match what the scan produces.
