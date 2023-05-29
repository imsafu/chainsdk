package sol

import (
	"encoding/hex"

	ag_binary "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
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

