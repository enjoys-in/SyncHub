defmodule WsElixirWeb.HealthController do
  use Phoenix.Controller, formats: [:json]

  def health(conn, _params) do
    json(conn, %{
      status: "ok",
      runtime: "elixir/beam",
      sse_subscribers: WsElixir.SSEBroker.total_subscribers(),
      timestamp: System.system_time(:millisecond)
    })
  end

  def stats(conn, _params) do
    json(conn, %{
      runtime: "elixir/beam",
      process_count: :erlang.system_info(:process_count),
      sse_subscribers: WsElixir.SSEBroker.total_subscribers(),
      memory_mb: Float.round(:erlang.memory(:total) / 1_048_576, 2),
      timestamp: System.system_time(:millisecond)
    })
  end
end
