.PHONY: tools fmt lint test coverage check build install-hooks bench bench-save bench-compare profile-cpu profile-mem

# Install/update tools
tools:
	go install github.com/incu6us/goimports-reviser/v3@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# Format all Go files
fmt: tools
	goimports-reviser -format -recursive .

# Lint
lint: tools
	golangci-lint run

# Run tests (use PKG=./path/to/package to test specific package)
test:
ifdef PKG
	go test -v $(PKG)
else
	go test ./...
endif

# Run tests with coverage (use PKG=./path/to/package for specific package)
coverage:
ifdef PKG
	go test -cover -coverprofile=coverage.out $(PKG)
else
	go test -cover -coverprofile=coverage.out ./...
endif
	go tool cover -func=coverage.out

# Format, lint, and test
check: fmt lint test

# Build (verify compilation)
build:
	go build ./...

# Install git hooks
install-hooks:
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit

# Run benchmarks with memory stats
bench:
	go test -bench=. -benchmem -count=10 ./...

# Save benchmark results to baseline
bench-save:
	@mkdir -p benchmarks
	go test -bench=. -benchmem -count=10 ./... > benchmarks/baseline.txt
	@echo "Baseline saved to benchmarks/baseline.txt"

# Compare current benchmarks against baseline
bench-compare:
	@if [ ! -f benchmarks/baseline.txt ]; then \
		echo "No baseline found. Run 'make bench-save' first."; \
		exit 1; \
	fi
	@mkdir -p benchmarks
	go test -bench=. -benchmem -count=10 ./... > benchmarks/current.txt
	benchstat benchmarks/baseline.txt benchmarks/current.txt

# Generate CPU profile (uses small file for faster iteration)
profile-cpu:
	@mkdir -p benchmarks
	go test -bench=BenchmarkDecode/small -benchmem -cpuprofile=benchmarks/cpu.prof -count=1
	go tool pprof -http=:8080 benchmarks/cpu.prof

# Generate memory profile
profile-mem:
	@mkdir -p benchmarks
	go test -bench=BenchmarkDecode/small -benchmem -memprofile=benchmarks/mem.prof -count=1
	go tool pprof -http=:8080 benchmarks/mem.prof
