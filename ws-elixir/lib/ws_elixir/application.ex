defmodule WsElixir.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    WsElixirWeb.RateLimiter.setup()

    children = [
      {Phoenix.PubSub, name: WsElixir.PubSub},
      WsElixir.ApiKeyStore,
      WsElixir.SSEBroker,
      WsElixirWeb.Endpoint
    ]

    opts = [strategy: :one_for_one, name: WsElixir.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
