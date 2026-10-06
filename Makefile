.PHONY: build test live run eval vet clean

build:
	go build -o bin/jokes ./cmd/jokes
	go build -o bin/jokes-mcp ./cmd/jokes-mcp
	go build -o bin/jokes-eval ./cmd/jokes-eval

test:
	go test ./...

live:
	LIVE=1 go test ./...

vet:
	go vet ./...

eval:
	go run ./cmd/jokes-eval -stage 0

run:
	go run ./cmd/jokes -stage 3 -trace

clean:
	rm -rf bin
