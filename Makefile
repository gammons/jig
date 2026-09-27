.PHONY: build test lint fmt-check check

build:
	go build -o bin/jig ./cmd/jig

test:
	go test ./... -race

lint:
	golangci-lint run

fmt-check:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "$$fmt_out"; \
		echo "gofmt: files need formatting"; \
		exit 1; \
	fi

check: build test lint fmt-check
