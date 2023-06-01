package sol

import (
	"context"
	"encoding/hex"

	ag_binary "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	lookup "github.com/gagliardetto/solana-go/programs/address-lookup-table"
)

type Transaction solana.Transaction

func (tx *Transaction) Unwrap() *solana.Transaction {
	return (*solana.Transaction)(tx)
}

func (tx *Transaction) UnmarshalText(text []byte) error {
	// assumes hex input
	input := string(text)

	data, err := hex.DecodeString(input)
	if err != nil {
		return err
		// data, err = base64.StdEncoding.DecodeString(input)
		// if err != nil {
		// 	return errors.New("invalid input neither hex nor base64")
		// }
	}

	dtx, err := DecodeTransaction(data)
	if err != nil {
		return err
	}

	*tx = (Transaction)(*dtx)

	return nil
}

func DecodeTransaction(data []byte) (*solana.Transaction, error) {
	return solana.TransactionFromDecoder(ag_binary.NewBinDecoder(data))
}

// this function may not work, since lookup table account may closed when we try to get it
func ProcessTransactionWithAddressLookups(txx solana.Transaction, rpcClient *rpc.Client) (solana.Transaction, error) {
	if !txx.Message.IsVersioned() {
		return txx, nil
	}

	tblKeys := txx.Message.GetAddressTableLookups().GetTableIDs()
	if len(tblKeys) == 0 {
		return txx, nil //no lookup tables in versioned transaction
	}

	if txx.Message.GetAddressTableLookups().NumLookups() == 0 {
		return txx, nil //no lookups in versioned transaction
	}
	resolutions := make(map[solana.PublicKey]solana.PublicKeySlice)
	for _, key := range tblKeys {

		info, err := rpcClient.GetAccountInfo(
			context.Background(),
			key,
		)
		if err != nil {
			return txx, err
		}

		tableContent, err := lookup.DecodeAddressLookupTableState(info.GetBinary())
		if err != nil {
			return txx, err
		}

		resolutions[key] = tableContent.Addresses
	}

	err := txx.Message.SetAddressTables(resolutions)
	if err != nil {
		return txx, err
	}

	err = txx.Message.ResolveLookups()
	if err != nil {
		return txx, err
	}

	return txx, nil
}

// see: https://github.com/solana-labs/solana-web3.js/blob/1cb3e8b2137df15d81a8a2da2a5dc74291114cac/packages/library-legacy/src/message/account-keys.ts
type V0AccountKeys struct {
	staticAccountKeys []solana.PublicKey
	loadedAddress     rpc.LoadedAddresses
}

func NewV0AccountKeys(accountKeys []solana.PublicKey, loadedAddresses rpc.LoadedAddresses) *V0AccountKeys {
	return &V0AccountKeys{
		staticAccountKeys: accountKeys,
		loadedAddress:     loadedAddresses,
	}
}

func (keys *V0AccountKeys) keySegments() [][]solana.PublicKey {
	ret := [][]solana.PublicKey{
		keys.staticAccountKeys,
	}
	ret = append(ret, keys.loadedAddress.Writable)
	ret = append(ret, keys.loadedAddress.ReadOnly)
	return ret
}

func (keys *V0AccountKeys) Get(index int) *solana.PublicKey {
	for _, keySegment := range keys.keySegments() {
		if index < len(keySegment) {
			return &keySegment[index]
		} else {
			index -= len(keySegment)
		}
	}
	return nil
}

func TransactionAccountsGetter(tx rpc.TransactionWithMeta, rpcClient *rpc.Client) func(index int) *solana.PublicKey {
	txx := tx.MustGetTransaction()

	if !txx.Message.IsVersioned() { // legacy tx
		keys := txx.Message.AccountKeys
		return func(index int) *solana.PublicKey {
			if index >= len(keys) {
				return nil
			}
			return &keys[index]
		}
	}

	keys := NewV0AccountKeys(txx.Message.AccountKeys, tx.Meta.LoadedAddresses)
	return keys.Get
}
