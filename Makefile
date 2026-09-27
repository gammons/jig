.PHONY: build test lint fmt-check check

build:
	go build -o bin/jig ./cmd/jig

# The jigtest-tagged packages build the scripted provider; e2e runs the
# binary it builds in TestMain, which the test cache cannot see, hence
# -count=1.
test:
	go test ./... -race
	go test -tags jigtest -race -count=1 ./e2e/... ./internal/client/llm/jigtest/...

lint:
	golangci-lint run
	golangci-lint run --build-tags jigtest

fmt-check:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "$$fmt_out"; \
		echo "gofmt: files need formatting"; \
		exit 1; \
	fi

check: build test lint fmt-check
