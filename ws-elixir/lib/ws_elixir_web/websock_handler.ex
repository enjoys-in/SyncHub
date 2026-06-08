defmodule WsElixirWeb.WebsockHandler do
  @behaviour Websock
  require Logger

  @impl true
  def init(state) do
    # Subscribe to the channel so we receive broadcasts
    Phoenix.PubSub.subscribe(WsElixir.PubSub, "channel:#{state.channel}")
    # Subscribe to API key revocations
    Phoenix.PubSub.subscribe(WsElixir.PubSub, "api_keys")
    
    # Broadcast join
    msg = %{
      "type" => "system",
      "payload" => %{"text" => "connected", "user_id" => state.client_id},
      "timestamp" => System.system_time(:millisecond)
    }
    
    join_msg = %{
      "type" => "user_joined",
      "user_id" => state.client_id,
      "payload" => %{"user_id" => state.client_id},
      "timestamp" => System.system_time(:millisecond)
    }
    Phoenix.PubSub.broadcast(WsElixir.PubSub, "channel:#{state.channel}", {:broadcast, join_msg})
    
    {:push, {:text, Jason.encode!(msg)}, state}
  end

  @impl true
  def handle_in({text, [opcode: :text]}, state) do
    case Jason.decode(text) do
      {:ok, %{"type" => "ping"}} ->
        {:push, {:text, Jason.encode!(%{"type" => "pong", "timestamp" => System.system_time(:millisecond)})}, state}

      {:ok, %{"type" => "room_message", "payload" => payload}} ->
        msg = %{
          "type" => "message",
          "user_id" => state.client_id,
          "channel" => state.channel,
          "payload" => payload,
          "timestamp" => System.system_time(:millisecond)
        }
        Phoenix.PubSub.broadcast(WsElixir.PubSub, "channel:#{state.channel}", {:broadcast, msg})
        {:ok, state}

      {:ok, %{"type" => "direct", "target_id" => target_id, "payload" => payload}} ->
        msg = %{
          "type" => "direct",
          "user_id" => state.client_id,
          "payload" => payload,
          "timestamp" => System.system_time(:millisecond)
        }
        Phoenix.PubSub.broadcast(WsElixir.PubSub, "client:#{target_id}", {:broadcast, msg})
        {:ok, state}

      {:ok, %{"type" => "user_info", "payload" => payload}} ->
        msg = %{
          "type" => "presence",
          "user_id" => state.client_id,
          "info" => payload,
          "timestamp" => System.system_time(:millisecond)
        }
        Phoenix.PubSub.broadcast(WsElixir.PubSub, "channel:#{state.channel}", {:broadcast, msg})
        {:ok, state}

      {:ok, payload} ->
        Logger.warning("Unknown message: #{inspect(payload)}")
        {:ok, state}

      _ ->
        {:ok, state}
    end
  end

  def handle_info(%Phoenix.Socket.Broadcast{event: _event, payload: payload}, state) do
    {:push, {:text, Jason.encode!(payload)}, state}
  end

  def handle_info({:broadcast, message}, state) do
    # Only forward if it's not from us, or if it's a message type that should be echoed
    # For simplicity, we just send all broadcasted messages to the client
    {:push, {:text, Jason.encode!(message)}, state}
  end

  def handle_info({:revoke_key, revoked_key}, state) do
    if state.api_key == revoked_key do
      Logger.info("[ws] forcefully disconnecting revoked api_key=#{revoked_key}")
      {:stop, :normal, state}
    else
      {:ok, state}
    end
  end

  def terminate(_reason, state) do
    leave_msg = %{
      "type" => "user_left",
      "user_id" => state.client_id,
      "payload" => %{"user_id" => state.client_id},
      "timestamp" => System.system_time(:millisecond)
    }
    Phoenix.PubSub.broadcast(WsElixir.PubSub, "channel:#{state.channel}", {:broadcast, leave_msg})
    :ok
  end
end
