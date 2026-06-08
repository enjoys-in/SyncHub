defmodule WsElixirWeb.RateLimiter do
  @moduledoc """
  A simple ETS-backed token bucket / window rate limiter plug.
  Limits requests per IP.
  """
  import Plug.Conn

  @table :rate_limit
  @limit 100
  @window_ms 60_000

  def init(opts), do: opts

  def call(conn, _opts) do
    ip = conn.remote_ip |> :inet.ntoa() |> to_string()

    if allowed?(ip) do
      conn
    else
      conn
      |> put_resp_content_type("application/json")
      |> send_resp(429, Jason.encode!(%{error: "rate limit exceeded"}))
      |> halt()
    end
  end

  def setup do
    if :ets.info(@table) == :undefined do
      :ets.new(@table, [:named_table, :public, read_concurrency: true, write_concurrency: true])
    end
  end

  def allowed?(ip) do
    now = System.system_time(:millisecond)
    window_start = now - @window_ms

    # Clean old requests occasionally (simple stochastic cleanup for this basic implementation)
    if :rand.uniform(10) == 1 do
      :ets.select_delete(@table, [{{:_, :"$1"}, [{:<, :"$1", window_start}], [true]}])
    end

    # Count requests in window
    count = :ets.select_count(@table, [{{{ip, :_}, :"$1"}, [{:>=, :"$1", window_start}], [true]}])

    if count < @limit do
      :ets.insert(@table, {{ip, :erlang.unique_integer()}, now})
      true
    else
      false
    end
  end
end
