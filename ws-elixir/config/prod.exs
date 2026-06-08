import Config

config :ws_elixir, WsElixirWeb.Endpoint,
  http: [ip: {0, 0, 0, 0}, port: 4000],
  check_origin: false,
  secret_key_base: System.get_env("SECRET_KEY_BASE") || "prod_secret_key_base_that_is_at_least_64_bytes_long_for_phoenix_framework_ok",
  server: true
