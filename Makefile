GO ?= go

# Every directory under cmd/ is a binary; they build to the repo root, which
# .gitignore already expects.
BINS := $(notdir $(wildcard cmd/*))

# The Monte Carlo tests are slow, and slower still on a small machine. The
# race detector instruments every read of the shared heightmap grid — hundreds
# of thousands per trial — so it runs with -short, skipping the heavy
# integration tests while still covering the worker pool, the field cache, and
# the server's job state. This is the same split CI uses.
TEST_TIMEOUT ?= 25m
RACE_TIMEOUT ?= 15m

.PHONY: all build clean fmt fmt-check vet test test-race check install help

all: build

## build: compile the commands
build: $(BINS)

.PHONY: $(BINS)
$(BINS):
	$(GO) build -o $@ ./cmd/$@

## install: install the commands into GOBIN
install:
	$(GO) install ./cmd/...

## fmt: rewrite sources with gofmt
fmt:
	gofmt -w .

## fmt-check: fail if anything needs gofmt
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "gofmt needs to run on:" >&2; \
		echo "$$out" >&2; \
		exit 1; \
	fi

## vet: run go vet
vet:
	$(GO) vet ./...

## test: run the full suite, including the Monte Carlo integration tests
test:
	$(GO) test -timeout $(TEST_TIMEOUT) ./...

## test-race: race-check everything but the heavy Monte Carlo tests
test-race:
	$(GO) test -race -short -timeout $(RACE_TIMEOUT) ./...

## check: everything CI runs
check: fmt-check vet build test test-race

## clean: remove build outputs
clean:
	rm -f $(BINS)
	$(GO) clean -testcache

## help: list targets
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST)
