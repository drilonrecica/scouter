.PHONY: lint test test-race run

lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l .; echo "gofmt needed"; exit 1)
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

run:
	SCOUTER_DATA_DIR=./.data go run ./cmd/scouter-aggregator
