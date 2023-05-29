package sol

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/samber/lo"

	ag_binary "github.com/gagliardetto/binary"
)

type UnmarshalWithBorshDecoder[B any] interface {
	UnmarshalWithDecoder(decoder *ag_binary.Decoder) (err error)
	*B // non-interface type constraint element, https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md#pointer-method-example
}

type AccountLoader[T any, PT UnmarshalWithBorshDecoder[T]] struct {
	programID     solana.PublicKey
	discriminator [8]byte
	rpc           *rpc.Client
}

func NewAccountLoader[T any, PT UnmarshalWithBorshDecoder[T]](programID solana.PublicKey, discriminator [8]byte, client *rpc.Client) *AccountLoader[T, PT] {
	return &AccountLoader[T, PT]{
		programID:     programID,
		discriminator: discriminator,
		rpc:           client,
	}
}

type AccountState[T any] struct {
	Address solana.PublicKey
	State   T
}

func (l *AccountLoader[T, PT]) DecodeAccount(data []byte) (*T, error) {
	t := new(T)
	pt := PT(t)
	err := pt.UnmarshalWithDecoder(ag_binary.NewBorshDecoder(data))
	return t, err
}

func (l *AccountLoader[T, PT]) Account(ctx context.Context, account solana.PublicKey) (AccountState[*T], error) {
	ret, err := l.rpc.GetAccountInfo(ctx, account)
	if err != nil {
		return AccountState[*T]{}, err
	}

	acc := ret.Value

	state, err := l.DecodeAccount(acc.Data.GetBinary())
	if err != nil {
		return AccountState[*T]{}, err
	}

	return AccountState[*T]{
		Address: account,
		State:   state,
	}, nil

}

func (l *AccountLoader[T, PT]) FilterAccounts(ctx context.Context, filters ...[]byte) (ret []AccountState[*T], err error) {
	filterBytes := l.discriminator[:]
	for i := 0; i < len(filters); i++ {
		filterBytes = append(filterBytes, filters[i]...)
	}

	accounts, err := l.rpc.GetProgramAccountsWithOpts(ctx, l.programID, &rpc.GetProgramAccountsOpts{
		Filters: []rpc.RPCFilter{
			{Memcmp: &rpc.RPCFilterMemcmp{
				Offset: 0,
				Bytes:  solana.Base58(filterBytes),
			}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("load accounts: %w", err)
	}

	for i := 0; i < len(accounts); i++ {
		t := accounts[i]
		state, err := l.DecodeAccount(t.Account.Data.GetBinary())
		if err != nil {
			return nil, err
		}

		ret = append(ret, AccountState[*T]{
			Address: t.Pubkey,
			State:   state,
		})
	}

	return
}

func (l *AccountLoader[T, PT]) FilterAccountsMap(ctx context.Context) (map[solana.PublicKey]AccountState[*T], error) {
	accounts, err := l.FilterAccounts(ctx)
	if err != nil {
		return nil, err
	}
	res := lo.KeyBy(accounts, func(t AccountState[*T]) solana.PublicKey { return t.Address })
	return res, nil
}

func (l *AccountLoader[T, PT]) MultipleAccounts(ctx context.Context, accounts ...solana.PublicKey) ([]AccountState[*T], error) {
	res, err := l.rpc.GetMultipleAccounts(ctx, accounts...)
	if err != nil {
		return nil, err
	}

	ret := make([]AccountState[*T], 0)
	for i, t := range res.Value {
		if t == nil {
			// emm, how to handle account that do not exists? for now return with State = nil
			ret = append(ret, AccountState[*T]{
				Address: accounts[i],
				State:   nil,
			})
			continue
		}

		state, err := l.DecodeAccount(t.Data.GetBinary())
		if err != nil {
			return nil, err
		}

		ret = append(ret, AccountState[*T]{
			Address: accounts[i],
			State:   state,
		})
	}
	return ret, nil
}

func (l *AccountLoader[T, PT]) MultipleAccountsMap(ctx context.Context, accounts ...solana.PublicKey) (map[solana.PublicKey]AccountState[*T], error) {
	data, err := l.MultipleAccounts(ctx, accounts...)
	if err != nil {
		return nil, err
	}
	res := lo.KeyBy(data, func(t AccountState[*T]) solana.PublicKey { return t.Address })
	return res, nil
}
