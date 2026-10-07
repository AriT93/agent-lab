.PHONY: build test live run eval vet clean py-test lg-dev lg-chat lg-ui rb-test rb-run rb-eval rb-web docker-build docker-up docker-down docker-smoke

build:
	go build -o bin/jokes ./cmd/jokes
	go build -o bin/jokes-mcp ./cmd/jokes-mcp
	go build -o bin/jokes-eval ./cmd/jokes-eval
	go build -o bin/jokes-web ./cmd/jokes-web

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

rb-test:
	cd ruby && bundle exec rake test

rb-run:
	cd ruby && bundle exec bin/jokes --stage 3 --trace

rb-eval:
	cd ruby && bundle exec bin/jokes-eval --stage 0

rb-web:
	cd ruby && bundle exec bin/jokes-web

# Stage 9: containers. The model stays on your Mac (see compose.yaml).
docker-build:
	docker compose build go ruby langgraph ui

docker-up:
	docker compose up -d --build go ruby langgraph ui
	@echo "go :8080  ruby :8081  langgraph :2024  chat-ui :3000"

docker-down:
	docker compose down

# No model needed: starts the apps, waits for their health checks, probes them.
docker-smoke:
	docker compose up -d --build --wait go ruby langgraph
	curl -fsS localhost:8080/healthz && echo && curl -fsS localhost:8081/healthz && echo && curl -fsS localhost:2024/ok && echo
	docker compose down

py-test:
	cd python && uv run pytest -q

# Stage 8: the LangGraph dev server (:2024) and LangChain's agent-chat-ui (:3000).
# Run each in its own terminal. If pnpm fails with "Cannot find matching keyid",
# that is the corepack key rotation bug: export COREPACK_INTEGRITY_KEYS=0.
lg-dev:
	cd python && uv run langgraph dev --no-browser

lg-chat:
	cd python && uv run python -m agent_lab.cli --trace

lg-ui:
	mkdir -p python/.cache
	test -d python/.cache/agent-chat-ui || git clone --depth 1 https://github.com/langchain-ai/agent-chat-ui python/.cache/agent-chat-ui
	cd python/.cache/agent-chat-ui && test -f .env || cp .env.example .env
	cd python/.cache/agent-chat-ui && pnpm install && pnpm dev

clean:
	rm -rf bin
