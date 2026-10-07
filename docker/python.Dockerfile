# Stage 9: the LangGraph agent server (stage 8) as a container.
#
# This runs `langgraph dev`, the same in-memory server used on your laptop:
# fine for local work and demos, NOT for production. LangGraph's production
# path is `langgraph build` / `langgraph up`, which store threads and
# checkpoints in Postgres and Redis. See docs/deploy.md.
FROM python:3.13-slim
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv
WORKDIR /app
# Dependencies first: this layer only rebuilds when the lockfile changes.
COPY python/pyproject.toml python/uv.lock ./
RUN uv sync --frozen --no-install-project
COPY python/ ./
RUN uv sync --frozen && useradd --system --create-home app && chown -R app /app
USER app
ENV PATH="/app/.venv/bin:$PATH"
EXPOSE 2024
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s \
  CMD python -c "import urllib.request as u; u.urlopen('http://127.0.0.1:2024/ok', timeout=3)"
CMD ["langgraph", "dev", "--host", "0.0.0.0", "--port", "2024", "--no-browser"]
