import Config

config :ws_elixir, WsElixirWeb.Endpoint,
  url: [host: "localhost"],
  render_errors: [formats: [json: WsElixirWeb.ErrorJSON]],
  pubsub_server: WsElixir.PubSub,
  server: true

config :logger, :console,
  format: "$time $metadata[$level] $message\n",
  metadata: [:request_id]

config :phoenix, :json_library, Jason

import_config "#{config_env()}.exs"
