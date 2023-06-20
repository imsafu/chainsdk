package tron

import (
	"crypto/sha256"

	"github.com/fbsobreira/gotron-sdk/pkg/proto/api"
	"google.golang.org/protobuf/proto"
)

func TXHash(tx *api.TransactionExtention) (trHash []byte, err error) {
	rawData, err := proto.Marshal(tx.GetTransaction().GetRawData())
	if err != nil {
		return
	}
	h := sha256.Sum256(rawData)
	return h[:], nil
}
