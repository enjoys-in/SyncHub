defmodule WsElixir.MixProject do
  use Mix.Project

  def project do
    [
      app: :ws_elixir,
      version: "1.0.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      mod: {WsElixir.Application, []},
      extra_applications: [:logger, :runtime_tools]
    ]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_pubsub, "~> 2.1"},
      {:jason, "~> 1.4"},
      {:bandit, "~> 1.5"},
      {:plug_cowboy, "~> 2.7"},
      {:cors_plug, "~> 3.0"}
    ]
  end
end
