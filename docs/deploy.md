# Deploying agent-lab (stage 10)

This stage is a description, not a live deployment. Stage 9 shows the apps
running in containers on your Mac. This page explains what changes when the
same apps run on a server for other people, and gives two ways to do it.

Nothing here was run on a cloud platform. Platform names are examples. Check
each platform's current documentation before you follow a step.

## What changes between a laptop and a server

The apps already follow some of the common pattern. Others still need work.

| Concern | Today | On a server |
|---|---|---|
| Process | One container per app, listens on 8080 | Same. Platforms run several copies. |
| Health | `GET /healthz` (Go, Ruby), `/ok` (LangGraph) | Platforms call these to decide when to restart or route traffic. |
| Config | Environment variables (`OLLAMA_HOST`, `SESSION_SECRET`) | Same. Secrets come from the platform's secret store, not the image. |
| Model | Ollama on your machine | A hosted model API, or Ollama on a machine you control. |
| Conversation state | In the app's memory, one entry per browser | An external store. Memory is lost on restart and not shared between copies. |
| Auth | None. Ports bind to 127.0.0.1. | Required. Anyone who can reach the app can spend your model budget. |
| Tracing | `-trace` output, per-reply trace in the page | Structured traces in a tracing service (LangSmith, OpenTelemetry). |
| Evals | Run by hand with `-n` | Stub-model tests on every commit. Real-model evals on a schedule. |
| Limits | Max steps per message, 8K context | Add per-user rate limits, request timeouts and a spending cap. |

The state row matters most. The Go and Ruby apps keep each conversation in
memory. With two copies behind a load balancer, a user's second message can
land on a copy that has never seen them. You fix that in one of two ways.
Pin each user to one copy (sticky sessions), or move the conversation to
Redis or Postgres. LangGraph already has the second design built in. It saves
checkpoints per thread, and its production server uses Postgres for them.

## Option 1: a container platform and a hosted model API

This is the most common setup for a product.

1. Build the image and push it to a registry the platform can read.
2. Create a service from the image on a platform such as Google Cloud Run,
   Fly.io or AWS ECS.
3. Set the model API key and `SESSION_SECRET` as secrets.
4. Point the platform's health check at `/healthz`.
5. Put authentication in front of the app.

Code changes needed first:

- **A model provider that is not Ollama.** The Go repo already has this in
  stage 6 (`internal/provider`). Ruby and Python call Ollama directly, so they
  need the same seam.
- **Shared conversation state** for the Go and Ruby apps, as described above.
- **A decision about cold starts.** Platforms that scale to zero stop your
  container when it is idle. The first request after that waits for the start.

For the LangGraph server, do not ship `langgraph dev`. The `Dockerfile` uses
it only for local runs. The production path is LangGraph's own build and
deploy tooling with Postgres and Redis. Read LangChain's current documentation
for its requirements and licensing.

You need a model API key for this option. A Claude Pro subscription does not
include one. You would create a key in the Anthropic Console, or use another
provider's API.

## Option 2: a machine you control, with Ollama in Compose

This keeps the local-model design from the rest of the repo.

1. Pick a Linux server with enough memory, ideally with an NVIDIA GPU. A
   CPU-only server works but is slow. A 9B model can take many seconds per
   token on a small VPS.
2. Install Docker and copy the repo (or just `compose.yaml` and the images).
3. Run `OLLAMA_HOST=http://ollama:11434 docker compose --profile ollama up -d`.
4. Pull a model inside the Ollama container with `docker compose exec ollama
   ollama pull <model>`, then start the apps with `-model <model>`.
5. Put a reverse proxy such as Caddy or Traefik in front for HTTPS and login.
   Do not publish the app ports to the internet.

Things to know:

- The `qwen3.5:9b-mlx` model is an Apple MLX build. It only runs through
  Ollama installed natively on macOS. A Linux server needs a model tag built
  for Linux. Check Ollama's library for what is available.
- Ollama inside Docker on a Mac has no GPU access. That is why Compose
  points at your host's Ollama by default.
- This option needs no API key and has no per-request bill. You pay for the
  machine and you do the patching.

## Choosing

| | Option 1: hosted API | Option 2: your own machine |
|---|---|---|
| Model quality | Best models available | Whatever fits your hardware |
| Cost | Per request | Fixed, plus your time |
| Speed | Fast | Depends on the GPU |
| Needs an API key | Yes | No |
| Operations work | Low | Higher |
| Data leaves your network | Yes | No |

A reasonable path: build and test with Option 2 on your own hardware, then
move to Option 1 once you want other people to use it.
