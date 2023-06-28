package hdw

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
)

// eth : "m/44'/60'/0'/0/0"
// tron: "m/44'/195'/0'/0/0"
func DeriveECDSAKey(path, mnemonic, password string) (*ecdsa.PrivateKey, error) {
	derivationPath, err := accounts.ParseDerivationPath(path)
	if err != nil {
		return nil, err
	}

	key, err := bip32.NewMasterKey(bip39.NewSeed(mnemonic, password))
	if err != nil {
		return nil, err
	}

	for _, pa := range derivationPath {
		key, err = key.NewChildKey(pa)
		if err != nil {
			return nil, err
		}
	}

	return crypto.ToECDSA(key.Key)
}
