.PHONY: build test live run vet clean

build:
	go build -o bin/jokes ./cmd/jokes

test:
	go test ./...

live:
	LIVE=1 go test ./...

vet:
	go vet ./...

run:
	go run ./cmd/jokes -stage 3 -trace

clean:
	rm -rf bin
