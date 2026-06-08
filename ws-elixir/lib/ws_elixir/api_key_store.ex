defmodule WsElixir.ApiKeyStore do
  @moduledoc """
  GenServer-backed API key store with domain validation.
  Persists keys to a JSON file. Same model as the Go broker.
  """
  use GenServer

  @file_path "data/api_keys.json"

  # ── Client API ──────────────────────────────────────────────

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def create(name, domains) do
    GenServer.call(__MODULE__, {:create, name, domains})
  end

  def validate(api_key, origin) do
    GenServer.call(__MODULE__, {:validate, api_key, origin})
  end

  def list do
    GenServer.call(__MODULE__, :list)
  end

  def revoke(api_key) do
    GenServer.call(__MODULE__, {:revoke, api_key})
  end

  # ── Server Callbacks ────────────────────────────────────────

  @impl true
  def init(_) do
    keys = load_keys()
    {:ok, %{keys: keys}}
  end

  @impl true
  def handle_call({:create, name, domains}, _from, state) do
    key = generate_key(32)

    api_key = %{
      "key" => key,
      "name" => name,
      "allowed_domains" => domains,
      "created_at" => System.system_time(:millisecond),
      "active" => true
    }

    new_keys = Map.put(state.keys, key, api_key)
    save_keys(new_keys)
    {:reply, {:ok, api_key}, %{state | keys: new_keys}}
  end

  @impl true
  def handle_call({:validate, api_key, origin}, _from, state) do
    case Map.get(state.keys, api_key) do
      nil ->
        {:reply, {:error, "invalid API key"}, state}

      %{"active" => false} ->
        {:reply, {:error, "API key is deactivated"}, state}

      key_data ->
        if origin == "" or origin == nil or matches_domain?(key_data["allowed_domains"], origin) do
          {:reply, {:ok, key_data}, state}
        else
          {:reply, {:error, "origin '#{origin}' not allowed"}, state}
        end
    end
  end

  @impl true
  def handle_call(:list, _from, state) do
    masked =
      state.keys
      |> Map.values()
      |> Enum.map(fn k ->
        key = k["key"]
        masked_key = String.slice(key, 0, 8) <> "..." <> String.slice(key, -4, 4)
        Map.put(k, "key", masked_key)
      end)

    {:reply, masked, state}
  end

  @impl true
  def handle_call({:revoke, api_key}, _from, state) do
    case Map.get(state.keys, api_key) do
      nil ->
        {:reply, :not_found, state}

      key_data ->
        updated = Map.put(key_data, "active", false)
        new_keys = Map.put(state.keys, api_key, updated)
        save_keys(new_keys)
        
        # Broadcast revocation to drop active connections
        Phoenix.PubSub.broadcast(WsElixir.PubSub, "api_keys", {:revoke_key, api_key})
        
        {:reply, :ok, %{state | keys: new_keys}}
    end
  end

  # ── Domain matching ─────────────────────────────────────────

  defp matches_domain?(domains, origin) do
    host = extract_host(origin)

    Enum.any?(domains, fn pattern ->
      pattern = String.trim(pattern) |> String.downcase()
      host_lower = String.downcase(host)

      cond do
        pattern == "*" -> true
        pattern == host_lower -> true
        String.starts_with?(pattern, "*.") ->
          suffix = String.slice(pattern, 1..-1//1)
          String.ends_with?(host_lower, suffix) or host_lower == String.slice(pattern, 2..-1//1)
        true -> false
      end
    end)
  end

  defp extract_host(origin) do
    case URI.parse(origin) do
      %URI{host: host} when is_binary(host) -> host
      _ -> origin |> String.split(":") |> List.first() |> String.trim()
    end
  end

  defp generate_key(length) do
    :crypto.strong_rand_bytes(length) |> Base.encode16(case: :lower)
  end

  defp load_keys do
    case File.read(@file_path) do
      {:ok, data} ->
        case Jason.decode(data) do
          {:ok, keys} -> keys
          _ -> %{}
        end
      _ -> %{}
    end
  end

  defp save_keys(keys) do
    File.mkdir_p!(Path.dirname(@file_path))
    case Jason.encode(keys, pretty: true) do
      {:ok, data} -> File.write(@file_path, data)
      _ -> :ok
    end
  end
end
