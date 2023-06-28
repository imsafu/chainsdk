package tron

import (
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fbsobreira/gotron-sdk/pkg/client"
	"github.com/google/wire"
	"google.golang.org/grpc"
)

type Config struct {
	GRPCAPI   string
	RPCAPI    string
	WalletKey hexutil.Bytes // optional
}

var Wires = wire.NewSet(
	ProvideGrpcClient,
	ProvideWallet,
	ProvideRPCClient,
	NewTokenCache,
)

func ProvideGrpcClient(cfg Config) (*client.GrpcClient, error) {
	c := client.NewGrpcClientWithTimeout(cfg.GRPCAPI, 30*time.Second)
	err := c.Start(grpc.WithInsecure())
	return c, err
}

type RPCClient ethclient.Client

func ProvideRPCClient(cfg Config) (*RPCClient, error) {
	c, e := ethclient.Dial(cfg.RPCAPI)
	return (*RPCClient)(c), e
}

func ProvideWallet(cfg Config) (*Wallet, error) {
	key, err := crypto.ToECDSA(cfg.WalletKey)
	if err != nil {
		return nil, err
	}
	grpcClient, err := ProvideGrpcClient(cfg)
	if err != nil {
		return nil, err
	}

	rpcClient, err := ProvideRPCClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Wallet{
		privateKey: key,
		Client:     grpcClient,
		RPC:        (*ethclient.Client)(rpcClient),
	}, nil
}

// shasta config
func ProvideTestConfig() Config {
	return Config{
		GRPCAPI: "grpc.shasta.trongrid.io:50051",
		RPCAPI:  "https://api.shasta.trongrid.io/jsonrpc",
	}
}
