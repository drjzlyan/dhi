BINARY := dhi

.PHONY: build run test verify goldens lint eval record clean

build:
	go build -o bin/$(BINARY) ./cmd/dhi

run:
	go run ./cmd/dhi

test:
	go test ./...

verify:
	@test -z "$$(gofmt -l . )" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test -race ./...

goldens:
	DHI_UPDATE_GOLDENS=1 go test ./internal/...

lint:
	golangci-lint run

# Live harness evals (F-065): each fixture runs through the raw CLI and
# through DHI; spends real tokens. DHI_EVAL_CLI / _MODEL / _TASK / _REPEAT / _OUT.
eval:
	DHI_EVAL=1 go test -count=1 -v -timeout 90m -run TestLive ./internal/evals

# README recordings of real flows (F-100): needs VHS (brew install vhs).
record:
	scripts/record.sh

clean:
	rm -rf bin/
