package tron

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fbsobreira/gotron-sdk/pkg/address"
	"github.com/fbsobreira/gotron-sdk/pkg/client"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/api"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Wallet struct {
	privateKey *ecdsa.PrivateKey
	Client     *client.GrpcClient
	RPC        *ethclient.Client
}

func (w *Wallet) SignAndSend(tx *api.TransactionExtention) (string, error) {
	tx, err := w.SignTx(tx)
	if err != nil {
		return "", err
	}
	return w.Send(tx)
}

func (w *Wallet) SignTx(txx *api.TransactionExtention) (*api.TransactionExtention, error) {
	trHash, err := TXHash(txx)
	if err != nil {
		return nil, err
	}
	signature, err := crypto.Sign(trHash, w.privateKey)
	if err != nil {
		return nil, err
	}
	txx.Transaction.Signature = append(txx.Transaction.Signature, signature)
	return txx, nil
}

func (w *Wallet) Send(tx *api.TransactionExtention) (string, error) {
	trHash, err := TXHash(tx)
	if err != nil {
		return "", err
	}

	result, err := w.Client.Broadcast(tx.GetTransaction())
	if err != nil {
		gRPCStatus, isgRPCError := status.FromError(err)
		if isgRPCError && gRPCStatus.Code() == codes.DeadlineExceeded {
			return "", fmt.Errorf("tron broadcast tx timeout")
		}

		return "", err
	}

	if result.Code != api.Return_SUCCESS {
		return "", fmt.Errorf("tron broadcast not success: %v", result)
	}

	txHash := hex.EncodeToString(trHash)
	return txHash, nil
}

func (w *Wallet) Address() address.Address {
	return address.PubkeyToAddress(w.privateKey.PublicKey)
}

func (w *Wallet) ConfirmTx(txHash string) (*core.ResourceReceipt, error) {
	checkInterval, timeout := time.Second*4, time.Minute*3
	confirmations := int64(20) // 20 blocks

	startedAt := time.Now()
	for {
		time.Sleep(checkInterval)
		txi, err := w.Client.GetTransactionInfoByID(txHash)

		if err == nil {
			nowBlock, err := w.Client.GetNowBlock()
			if err != nil {
				return nil, err
			}

			if nowBlock.BlockHeader.RawData.Number-txi.BlockNumber < confirmations {
				continue
			}

			return txi.Receipt, nil
		}

		if !strings.Contains(err.Error(), "transaction info not found") {
			return nil, err
		}

		if time.Since(startedAt) > timeout {
			return nil, fmt.Errorf("confirm tx timeout %s", txHash)
		}
	}
}

// func (w *Wallet) EstimateCost(target address.Address, calldata []byte) (*big.Int, error) {
// 	trigger := &core.TriggerSmartContract{
// 		OwnerAddress:    address.PubkeyToAddress(w.PrivateKey.PublicKey),
// 		ContractAddress: target,
// 		Data:            calldata,
// 	}
// 	txx, err := w.Client.Client.TriggerContract(context.Background(), trigger)
// 	if err != nil {
// 		return nil, err
// 	}
// 	txx.Transaction.RawData.FeeLimit = 1e8

// 	constEst, err := w.Client.Client.TriggerConstantContract(context.Background(), trigger)
// 	if err != nil {
// 		return nil, err
// 	}

// 	price, err := w.Client.SuggestGasPrice(context.Background()) // TODO this is a evm rpc ...ethclient.Client
// 	if err != nil {
// 		return nil, err
// 	}
// 	return new(big.Int).Mul(price, big.NewInt(constEst.EnergyUsed)), nil

// 	// est, err := w.Client.Client.EstimateEnergy(context.Background(), trigger) //use TriggerConstantContract instead
// 	// if err != nil {
// 	// 	return nil, err
// 	// }
// 	// fmt.Println("EnergyRequired", est.EnergyRequired)
// }
