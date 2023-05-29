package sol

import (
	"encoding/json"

	"github.com/gagliardetto/solana-go"
	"github.com/samber/lo"
)

type JSONPrivateKey solana.PrivateKey

func (s JSONPrivateKey) JSON() string {
	str, _ := s.MarshalJSON()
	return string(str)
}

func (s JSONPrivateKey) Value() solana.PrivateKey {
	return solana.PrivateKey(s)
}

func (s *JSONPrivateKey) UnmarshalText(text []byte) error {
	return json.Unmarshal(text, &s)
}

func (t JSONPrivateKey) MarshalJSON() ([]byte, error) {
	// output as array of integers, per the retarded SOL private key JSON file convention
	return json.Marshal(lo.Map(t, func(b byte, _ int) uint { return uint(b) }))
}
