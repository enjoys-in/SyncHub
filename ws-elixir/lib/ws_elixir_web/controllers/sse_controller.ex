defmodule WsElixirWeb.SSEController do
  use Phoenix.Controller, formats: [:json]

  def subscribe(conn, params) do
    api_key = params["api_key"] || get_req_header_first(conn, "x-api-key") || ""
    channel = params["channel"] || ""
    origin = get_req_header_first(conn, "origin") || get_req_header_first(conn, "referer") || ""

    cond do
      api_key == "" ->
        conn |> put_status(401) |> json(%{error: "api_key required"})

      channel == "" ->
        conn |> put_status(400) |> json(%{error: "channel required"})

      true ->
        case WsElixir.ApiKeyStore.validate(api_key, origin) do
          {:ok, _} -> stream_sse(conn, channel, api_key)
          {:error, reason} -> conn |> put_status(403) |> json(%{error: reason})
        end
    end
  end

  defp stream_sse(conn, channel, api_key) do
    conn =
      conn
      |> put_resp_header("content-type", "text/event-stream")
      |> put_resp_header("cache-control", "no-cache")
      |> put_resp_header("connection", "keep-alive")
      |> put_resp_header("x-accel-buffering", "no")
      |> send_chunked(200)

    # Subscribe to SSE broker and api_keys for revocation
    WsElixir.SSEBroker.subscribe(channel)
    Phoenix.PubSub.subscribe(WsElixir.PubSub, "api_keys")

    # Send connected event
    connected = Jason.encode!(%{channel: channel, timestamp: System.system_time(:millisecond)})
    {:ok, conn} = chunk(conn, "event: connected\ndata: #{connected}\n\n")

    # Stream loop
    sse_loop(conn, channel, api_key)
  end

  defp sse_loop(conn, channel, api_key) do
    receive do
      {:sse_event, message} ->
        data = if is_binary(message), do: message, else: Jason.encode!(message)

        case chunk(conn, "event: message\ndata: #{data}\n\n") do
          {:ok, conn} -> sse_loop(conn, channel, api_key)
          {:error, _} -> WsElixir.SSEBroker.unsubscribe(channel)
        end
        
      {:revoke_key, revoked_key} ->
        if api_key == revoked_key do
          # Force disconnect
          WsElixir.SSEBroker.unsubscribe(channel)
          # We just exit the loop to close the connection
          conn
        else
          sse_loop(conn, channel, api_key)
        end
    after
      30_000 ->
        # Send keepalive comment every 30s
        case chunk(conn, ": keepalive\n\n") do
          {:ok, conn} -> sse_loop(conn, channel, api_key)
          {:error, _} -> WsElixir.SSEBroker.unsubscribe(channel)
        end
    end
  end

  defp get_req_header_first(conn, header) do
    case Plug.Conn.get_req_header(conn, header) do
      [val | _] -> val
      _ -> nil
    end
  end
end
