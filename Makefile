BINARY := dhi

.PHONY: build run test verify goldens lint clean

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

clean:
	rm -rf bin/
