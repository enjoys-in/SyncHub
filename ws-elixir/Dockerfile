FROM elixir:1.17

ENV HEX_HTTP_TIMEOUT=120000
ENV HEX_HTTP_CONCURRENCY=1

RUN mix local.hex --force && mix local.rebar --force

WORKDIR /app

# Install deps first (cache layer)
COPY mix.exs ./
RUN mix deps.get --only dev && mix deps.compile

# Copy source
COPY config ./config
COPY lib ./lib

# Compile
ENV MIX_ENV=dev
RUN mix compile

EXPOSE 4000

CMD ["mix", "phx.server"]
