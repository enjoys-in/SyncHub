defmodule WsElixirWeb.Router do
  use Phoenix.Router

  pipeline :api do
    plug :accepts, ["json"]
  end

  scope "/", WsElixirWeb do
    pipe_through :api

    # API Key management
    post "/api/keys", ApiKeyController, :create
    get "/api/keys", ApiKeyController, :index
    delete "/api/keys/revoke", ApiKeyController, :revoke

    # Publish message to channel
    post "/publish", PublishController, :publish

    # SSE subscribe
    get "/subscribe", SSEController, :subscribe

    # Health & stats
    get "/health", HealthController, :health
    get "/stats", HealthController, :stats
  end
end
