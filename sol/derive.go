package sol

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/gagliardetto/solana-go"
	"github.com/samber/lo"
)

// DeriveSigner extends a signer with subkeys
func DeriveSigner(signer Signer, basekey solana.PublicKey) (*Derivation, error) {
	skey := signer.PrivateKey(basekey)

	if skey == nil {
		return nil, errors.New("signer derive base key not found")
	}

	d := NewSubkeys(*skey)

	de := &Derivation{
		Extension: ExtendSigner(signer, d),
		subkeys:   d,
	}

	return de, nil
}

var _ Signer = (*Derivation)(nil)

type Derivation struct {
	// where's the extension?
	Extension
	subkeys *Subkeys
}

type Deriver interface {
	Derive(seeds ...[]byte) solana.PublicKey
}

func (d *Derivation) Derive(seeds ...[]byte) solana.PublicKey {
	return d.subkeys.Derive(seeds...)
}

var _ Signer = (*Subkeys)(nil)

func NewSubkeys(secret solana.PrivateKey) *Subkeys {
	return &Subkeys{key: secret, keys: make(map[solana.PublicKey]solana.PrivateKey)}
}

// Subkeys derive keys from seeds
type Subkeys struct {
	key solana.PrivateKey

	keys map[solana.PublicKey]solana.PrivateKey
	mu   sync.Mutex
}

func (d *Subkeys) Copy() *Subkeys {
	d.mu.Lock()
	defer d.mu.Unlock()

	keys := make(map[solana.PublicKey]solana.PrivateKey)
	for k, v := range d.keys {
		keys[k] = v
	}

	return &Subkeys{
		key:  d.key,
		keys: keys,
	}
}

func (d *Subkeys) Derive(seeds ...[]byte) solana.PublicKey {
	flatSeed := lo.Flatten(append([][]byte{d.key}, seeds...))
	hash := sha256.Sum256(flatSeed)
	sk := privateKeyFromSeed(hash[:])

	d.mu.Lock()
	defer d.mu.Unlock()

	pk := sk.PublicKey()
	_, found := d.keys[pk]
	if !found {
		d.keys[pk] = sk
	}

	return pk
}

func (d *Subkeys) SignTx(tx *solana.Transaction) error {
	_, err := tx.Sign(d.PrivateKey)
	return err
}

func (d *Subkeys) PrivateKey(pubkey solana.PublicKey) *solana.PrivateKey {
	d.mu.Lock()
	defer d.mu.Unlock()

	sk, found := d.keys[pubkey]
	if !found {
		return nil
	}
	return &sk
}

func privateKeyFromSeed(seed []byte) solana.PrivateKey {
	pk := ed25519.NewKeyFromSeed(seed)
	return solana.PrivateKey(pk)
}
