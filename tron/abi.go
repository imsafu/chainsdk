package tron

import (
	"github.com/fbsobreira/gotron-sdk/pkg/contract"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
)

func MustABI(abiJSON string) *core.SmartContract_ABI {
	a, err := contract.JSONtoABI(abiJSON)
	if err != nil {
		panic(err)
	}
	return a
}
