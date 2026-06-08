defmodule WsElixir.SSEBroker do
  @moduledoc """
  Manages SSE (Server-Sent Events) subscribers per channel.
  Subscribers register their pid and receive messages via send/2.
  """
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def subscribe(channel) do
    GenServer.call(__MODULE__, {:subscribe, channel, self()})
  end

  def unsubscribe(channel) do
    GenServer.cast(__MODULE__, {:unsubscribe, channel, self()})
  end

  def publish(channel, message) do
    GenServer.call(__MODULE__, {:publish, channel, message})
  end

  def total_subscribers do
    GenServer.call(__MODULE__, :total)
  end

  # ── Server ──────────────────────────────────────────────────

  @impl true
  def init(_) do
    {:ok, %{channels: %{}}}
  end

  @impl true
  def handle_call({:subscribe, channel, pid}, _from, state) do
    Process.monitor(pid)
    subs = Map.get(state.channels, channel, MapSet.new()) |> MapSet.put(pid)
    new_channels = Map.put(state.channels, channel, subs)
    {:reply, :ok, %{state | channels: new_channels}}
  end

  @impl true
  def handle_call({:publish, channel, message}, _from, state) do
    subs = Map.get(state.channels, channel, MapSet.new())
    count = MapSet.size(subs)

    Enum.each(subs, fn pid ->
      send(pid, {:sse_event, message})
    end)

    {:reply, count, state}
  end

  @impl true
  def handle_call(:total, _from, state) do
    total = state.channels |> Map.values() |> Enum.map(&MapSet.size/1) |> Enum.sum()
    {:reply, total, state}
  end

  @impl true
  def handle_cast({:unsubscribe, channel, pid}, state) do
    subs = Map.get(state.channels, channel, MapSet.new()) |> MapSet.delete(pid)

    new_channels =
      if MapSet.size(subs) == 0,
        do: Map.delete(state.channels, channel),
        else: Map.put(state.channels, channel, subs)

    {:noreply, %{state | channels: new_channels}}
  end

  @impl true
  def handle_info({:DOWN, _ref, :process, pid, _reason}, state) do
    # Auto-cleanup when subscriber process dies
    new_channels =
      state.channels
      |> Enum.map(fn {ch, subs} -> {ch, MapSet.delete(subs, pid)} end)
      |> Enum.reject(fn {_ch, subs} -> MapSet.size(subs) == 0 end)
      |> Map.new()

    {:noreply, %{state | channels: new_channels}}
  end
end
