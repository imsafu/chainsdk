package sol

import (
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/google/wire"
	"github.com/mr-tron/base58"
)

type Config struct {
	RPCHost  string
	Seed     string // base58 encoded seed
	Wallet   string // base58 encoded private key
	Provider ProviderConfig
}

func (cfg Config) DecodeSeed() ([]byte, error) {
	return base58.Decode(cfg.Seed)
}

func ProvideClient(cfg Config) *rpc.Client {
	return rpc.New(cfg.RPCHost)
}

func ProvideSolanaSubkeys(cfg Config) (*Subkeys, error) {
	k, err := solana.PrivateKeyFromBase58(cfg.Wallet)
	if err != nil {
		return nil, err
	}

	return NewSubkeys(k), nil
}

func ProviderSolanaSigner(cfg Config) (Signer, error) {
	k, err := solana.PrivateKeyFromBase58(cfg.Wallet)
	if err != nil {
		return nil, err
	}

	return PrivateKeysFrom(k), nil
}

var Wires = wire.NewSet(
	NewProvider,
	ProvideClient,
	ProvideSolanaSubkeys,
	ProviderSolanaSigner,

	wire.FieldsOf(new(Config), "Provider"),
)

func ProvideTestValidatorConfig() Config {
	w := solana.NewWallet().PrivateKey
	return Config{
		RPCHost: "http://localhost:8899",
		Wallet:  base58.Encode(w),
		Seed:    "2VfUX",
		Provider: ProviderConfig{
			Debug:         true,
			Confirmations: 3,
		},
	}
}
