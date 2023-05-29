package sol

import (
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/google/wire"
)

type Config struct {
	RPCHost  string
	Provider ProviderConfig
}

func ProvideClient(cfg Config) *rpc.Client {
	return rpc.New(cfg.RPCHost)
}

var Wires = wire.NewSet(
	NewProvider,
	ProvideClient,
	wire.FieldsOf(new(Config), "Provider"),
)

func ProvideTestValidatorConfig() Config {
	return Config{
		RPCHost: "http://localhost:8899",
		Provider: ProviderConfig{
			Debug:         true,
			Confirmations: 3,
		},
	}
}
