.PHONY: all test bench vet clean

all: vet test

test:
	go test -race -timeout 120s ./...

bench:
	go test -bench=. -benchmem -run=^$$ ./...

vet:
	@output=$$(go vet ./... 2>&1 || true); \
	filtered=$$(printf '%s\n' "$$output" | grep -v "ctx.go:.*possible misuse of unsafe.Pointer" || true); \
	if [ -n "$$filtered" ]; then \
		echo "$$filtered"; \
		exit 1; \
	fi

clean:
	rm -f coverage.out