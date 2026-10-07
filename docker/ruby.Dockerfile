# Stage 9: the Ruby web UI (stage 7) as a container.
#
# The builder stage has a compiler for gems with C extensions (puma); the final
# stage only gets the installed gems and the app, not the compiler.
FROM ruby:3.4-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends build-essential && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY ruby/Gemfile ruby/Gemfile.lock ./
# Tests stay out of the production image; `--target test` below adds them back.
RUN bundle config set --local without test && bundle install

# `docker build --target test -f docker/ruby.Dockerfile .` runs the unit tests
# on the same Ruby the image ships, so a Ruby upgrade can't break silently.
FROM build AS test
COPY ruby/ ./
RUN bundle config unset --local without && bundle install && bundle exec rake test

# Last on purpose: a plain `docker build` or `compose build` builds the final stage.
FROM ruby:3.4-slim AS app
WORKDIR /app
COPY --from=build /usr/local/bundle /usr/local/bundle
COPY ruby/ ./
RUN bundle config set --local without test && useradd --system --create-home app && chown -R app /app
USER app
# production: no development-only host restrictions or error pages.
ENV APP_ENV=production
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s \
  CMD ruby -rnet/http -e 'exit(Net::HTTP.get_response(URI("http://127.0.0.1:8080/healthz")).is_a?(Net::HTTPOK) ? 0 : 1)'
CMD ["bundle", "exec", "bin/jokes-web", "--bind", "0.0.0.0", "--port", "8080"]
