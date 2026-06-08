defmodule WsElixirWeb.PublishController do
  use Phoenix.Controller, formats: [:json]

  def publish(conn, %{"channel" => channel} = params) do
    api_key = get_api_key(conn)
    origin = get_req_header_first(conn, "origin") || get_req_header_first(conn, "referer") || ""

    case WsElixir.ApiKeyStore.validate(api_key, origin) do
      {:ok, _} ->
        event = params["event"] || "message"
        data = params["data"] || %{}

        msg = %{
          "type" => event,
          "channel" => channel,
          "payload" => data,
          "timestamp" => System.system_time(:millisecond)
        }

        # Deliver to Phoenix Channel subscribers (WebSocket)
        WsElixirWeb.Endpoint.broadcast("channel:#{channel}", event, msg)

        # Deliver to SSE subscribers
        sse_count = WsElixir.SSEBroker.publish(channel, msg)

        json(conn, %{
          status: "published",
          channel: channel,
          sse_delivered: sse_count
        })

      {:error, reason} ->
        conn |> put_status(403) |> json(%{error: reason})
    end
  end

  def publish(conn, _params) do
    conn |> put_status(400) |> json(%{error: "channel required"})
  end

  defp get_api_key(conn) do
    case get_req_header_first(conn, "x-api-key") do
      nil -> conn.query_params["api_key"] || ""
      key -> key
    end
  end

  defp get_req_header_first(conn, header) do
    case Plug.Conn.get_req_header(conn, header) do
      [val | _] -> val
      _ -> nil
    end
  end
end
