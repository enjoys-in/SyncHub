defmodule WsElixirWeb.ApiKeyController do
  use Phoenix.Controller, formats: [:json]

  def create(conn, %{"name" => name, "domains" => domains}) do
    case WsElixir.ApiKeyStore.create(name, domains) do
      {:ok, api_key} -> json(conn, api_key)
      {:error, reason} -> conn |> put_status(400) |> json(%{error: reason})
    end
  end

  def create(conn, _params) do
    conn |> put_status(400) |> json(%{error: "name and domains required"})
  end

  def index(conn, _params) do
    json(conn, WsElixir.ApiKeyStore.list())
  end

  def revoke(conn, %{"key" => key}) do
    case WsElixir.ApiKeyStore.revoke(key) do
      :ok -> json(conn, %{status: "revoked"})
      :not_found -> conn |> put_status(404) |> json(%{error: "key not found"})
    end
  end

  def revoke(conn, _), do: conn |> put_status(400) |> json(%{error: "key param required"})
end
