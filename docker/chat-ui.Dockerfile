# Stage 9: LangChain's agent-chat-ui, the browser front end for the LangGraph
# server. It is someone else's project, so we clone it at build time instead of
# copying it into this repo. Pin CHAT_UI_REF to a commit or tag for repeatable builds.
FROM node:22-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
# npm, not corepack: corepack can fail verifying pnpm's signing keys.
RUN npm install -g pnpm@10.5.1
ARG CHAT_UI_REF=main
WORKDIR /ui
RUN git clone https://github.com/langchain-ai/agent-chat-ui . && git checkout "$CHAT_UI_REF" && pnpm install --frozen-lockfile
# The browser, not this container, calls the LangGraph server, so this is the
# address as seen from your machine.
ENV NEXT_PUBLIC_API_URL=http://localhost:2024 NEXT_PUBLIC_ASSISTANT_ID=agent
EXPOSE 3000
CMD ["pnpm", "dev", "-H", "0.0.0.0"]
