# Stage 9: the Go web UI (stage 7) as a small container.
#
# Two stages: build with the full Go toolchain, then copy the single static
# binary into an image with nothing else in it (no shell, no package manager).
# Less to patch, less to attack, a ~35 MB image.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/jokes-web ./cmd/jokes-web

# distroless/static has CA certificates (the joke APIs are HTTPS) and a
# non-root user, and nothing else.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/jokes-web /jokes-web
EXPOSE 8080
# Inside a container the app must listen on all interfaces; the port mapping
# decides what the outside world can reach. The image has no curl, so the app
# carries its own health probe.
HEALTHCHECK --interval=15s --timeout=5s --start-period=5s CMD ["/jokes-web", "-healthcheck", "-addr", "0.0.0.0:8080"]
ENTRYPOINT ["/jokes-web", "-addr", "0.0.0.0:8080"]
