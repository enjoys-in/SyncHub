defmodule WsElixirWeb.Endpoint do
  use Phoenix.Endpoint, otp_app: :ws_elixir

  plug WsElixirWeb.RateLimiter

  # Raw WebSocket Upgrade
  plug :ws_upgrade

  defp ws_upgrade(%{path_info: ["ws"]} = conn, _opts) do
    conn = fetch_query_params(conn)
    api_key = conn.query_params["api_key"] || ""
    channel = conn.query_params["channel"] || "default"
    client_id = conn.query_params["client_id"] || "anon-#{System.system_time(:nanosecond)}"
    
    # We need Origin validation
    origin = case Plug.Conn.get_req_header(conn, "origin") do
      [val | _] -> val
      _ -> ""
    end

    case WsElixir.ApiKeyStore.validate(api_key, origin) do
      {:ok, _} ->
        state = %{api_key: api_key, channel: channel, client_id: client_id}
        conn
        |> WebSockAdapter.upgrade(WsElixirWeb.WebsockHandler, state, timeout: 60_000)
        |> halt()
      {:error, reason} ->
        conn |> send_resp(403, Jason.encode!(%{error: reason})) |> halt()
    end
  end
  defp ws_upgrade(conn, _opts), do: conn

  plug Plug.RequestId
  plug Plug.Logger

  plug Plug.Parsers,
    parsers: [:json],
    pass: ["application/json"],
    json_decoder: Jason

  plug CORSPlug,
    origin: ["*"],
    methods: ["GET", "POST", "DELETE", "OPTIONS"],
    headers: ["Content-Type", "Authorization", "X-API-Key"]

  plug WsElixirWeb.Router
end
