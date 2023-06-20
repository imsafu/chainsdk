package tron

import (
	"bytes"
	"database/sql/driver"
	"fmt"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/fbsobreira/gotron-sdk/pkg/address"
)

type Address address.Address

func (a Address) Normalize() address.Address {
	return address.Address(a)
}

func (a Address) Equals(b Address) bool {
	return bytes.Equal([]byte(a), []byte(b))
}

func (a Address) String() string {
	return address.Address(a).String()
}

func (a Address) MarshalText() (text []byte, err error) {
	return []byte(address.Address(a).String()), nil
}

func (a *Address) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	addr, err := address.Base58ToAddress(string(text))
	if err != nil {
		return err
	}
	*a = Address(addr)
	return nil
}

func (k *Address) Scan(src interface{}) error {
	if s, ok := src.(string); ok {
		add, err := address.Base58ToAddress(s)
		if err != nil {
			return err
		}
		*k = Address(add)
		return nil
	}
	return fmt.Errorf("unable to scan tron address, unknown value type: %T", src)
}

func (k Address) Value() (driver.Value, error) {
	return k.Normalize().String(), nil
}

func MustAddress(add string) address.Address {
	addr, err := address.Base58ToAddress(add)
	if err != nil {
		panic(fmt.Errorf("failed to convert address: %w", err))
	}

	return addr
}

func ToETHAddress(addr address.Address) ethcommon.Address {
	var add ethcommon.Address
	copy(add[:], addr[1:])
	return add
}

func FromETHAddress(add ethcommon.Address) address.Address {
	tronAddress := make([]byte, 0)
	tronAddress = append(tronAddress, address.TronBytePrefix)
	tronAddress = append(tronAddress, add.Bytes()...)
	return tronAddress
}
