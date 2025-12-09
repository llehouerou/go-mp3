# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with this codebase.

## Project Overview

go-mp3 is an MP3 decoder in pure Go based on PDMP3. It provides a simple API to decode MP3 files into raw PCM audio data.

## Development Commands

**Always use the Makefile targets for development tasks:**

```bash
make fmt        # Format all Go files (required before commits)
make lint       # Run golangci-lint
make test       # Run all tests
make check      # Format, lint, and test (runs all three)
make build      # Verify compilation
make coverage   # Run tests with coverage report
```

To test a specific package:
```bash
make test PKG=./internal/frame
make coverage PKG=./internal/bits
```

## Code Style

- Run `make fmt` before committing - this is enforced by the pre-commit hook
- The pre-commit hook runs `make check` (format, lint, test) on every commit

## Project Structure

- `decode.go`, `source.go` - Main public API (Decoder type)
- `internal/` - Internal packages:
  - `bits/` - Bit-level reading utilities
  - `consts/` - Constants and lookup tables
  - `frame/` - MP3 frame decoding
  - `frameheader/` - Frame header parsing
  - `huffman/` - Huffman decoding tables
  - `imdct/` - Inverse modified discrete cosine transform
  - `maindata/` - Main audio data and scale factors
  - `sideinfo/` - Side information parsing
- `example/` - Example usage with oto audio library
