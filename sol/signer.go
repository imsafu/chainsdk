package sol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"path"
	"sort"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/samber/lo"
)

// type TxSigner interface {
// SignTx(tx *solana.Transaction) error
// }

type Signer interface {
	SignTx(tx *solana.Transaction) error
	PrivateKey(pubkey solana.PublicKey) *solana.PrivateKey
}

var _ Signer = (PrivateKeys)(nil)

// FakeSigner may be used to fake signatures. Useful to estimate the size of a
// tx by filling the signatures out with fake ones.
//
// The same publickey will always map to a unique fake private key.
type FakeSigner struct{}

func (s *FakeSigner) SignTx(tx *solana.Transaction) error {
	_, err := tx.Sign(s.PrivateKey)
	return err
}

func (s *FakeSigner) PrivateKey(pubkey solana.PublicKey) *solana.PrivateKey {
	hash := sha256.Sum256(pubkey[:])
	pk := privateKeyFromSeed(hash[:])

	return &pk
}

type PrivateKeys map[solana.PublicKey]solana.PrivateKey

func PrivateKeyFromFile(keypath string) (solana.PrivateKey, error) {
	// Try reading it as JSON file, or as base58 encoded string
	key, err := solana.PrivateKeyFromSolanaKeygenFile(keypath)
	if err == nil {
		return key, nil
	}

	// try base58 decode
	keybuf, err := ioutil.ReadFile(keypath)
	if err != nil {
		return nil, err
	}

	key, err = solana.PrivateKeyFromBase58(string(keybuf))
	if err != nil {
		return nil, fmt.Errorf("invalid private key file: %s, %w", keypath, err)
	}

	return key, nil
}

func PrivateKeysFromDir(dir string) (PrivateKeys, error) {
	fs, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load private keys from dir: %w", err)
	}

	var keys []solana.PrivateKey

	for _, f := range fs {
		if f.IsDir() {
			continue
		}

		keypath := path.Join(dir, f.Name())
		key, err := PrivateKeyFromFile(keypath)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	return PrivateKeysFrom(keys...), nil
}

func PrivateKeysFrom(keys ...solana.PrivateKey) PrivateKeys {
	mkeys := make(PrivateKeys, len(keys))

	for _, key := range keys {
		mkeys[key.PublicKey()] = key
	}

	return mkeys
}

func (sks PrivateKeys) SignTx(tx *solana.Transaction) error {
	_, err := tx.Sign(sks.PrivateKey)
	return err
}

func (sks PrivateKeys) ExportHex() string {
	keys := lo.Map(lo.Values(sks), func(k solana.PrivateKey, _ int) string {
		return hex.EncodeToString(k)
	})

	sort.Strings(keys)

	return strings.Join(keys, ":")
}

func (sks PrivateKeys) Add(sk solana.PrivateKey) {
	sks[sk.PublicKey()] = sk
}

func (sks PrivateKeys) PrivateKey(pk solana.PublicKey) *solana.PrivateKey {
	sk, ok := sks[pk]

	if !ok {
		return nil
	}

	return &sk
}

type Extension struct {
	self Signer
	base Signer
}

func (e Extension) SignTx(tx *solana.Transaction) error {
	_, err := tx.Sign(e.PrivateKey)
	return err
}

func (e Extension) PrivateKey(pk solana.PublicKey) *solana.PrivateKey {
	sk := e.self.PrivateKey(pk)
	if sk != nil {
		return sk
	}

	return e.base.PrivateKey(pk)
}

// Extend creates a Signer with the additional keys added to its lookup chain
func Extend(base Signer, newSks ...solana.PrivateKey) Signer {
	self := PrivateKeysFrom(newSks...)
	return Extension{self: self, base: base}
}

func ExtendSigner(base Signer, extension Signer) Extension {
	return Extension{self: extension, base: base}
}
