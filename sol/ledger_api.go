package sol

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/gagliardetto/solana-go"
	"github.com/go-resty/resty/v2"
)

var _ Signer = (*LedgerAPI)(nil)

type HDWallet struct {
	HDPath string           `json:"hdpath"`
	Pubkey solana.PublicKey `json:"pubkey"`
}

type hdWalletPaths map[solana.PublicKey]string

func NewLedgerAPI(base url.URL, wallets []HDWallet) *LedgerAPI {
	api := resty.New()
	api.SetBaseURL(base.String())

	m := make(hdWalletPaths)

	for _, w := range wallets {
		m[w.Pubkey] = w.HDPath
	}

	return &LedgerAPI{api: api, walletPaths: m}
}

type LedgerAPI struct {
	api         *resty.Client
	walletPaths hdWalletPaths
}

type ledgerSignRequest struct {
	HDPath string `json:"hdpath"`
	Msg    []byte `json:"msg"`
}

type ledgerSignResult struct {
	Sig solana.Signature `json:"sig"`
}

type LedgerErrResponse struct {
	Error string `json:"error"`
	Retry bool   `json:"retry"`
}

func (l *LedgerAPI) SignAs(signer solana.PublicKey, msg []byte) (out solana.Signature, err error) {

	var result ledgerSignResult

	hdpath, ok := l.walletPaths[signer]

	if !ok {
		return solana.Signature{}, errors.New("no HD path for signer")
	}

	var errBody LedgerErrResponse
	// TODO: retry if it's a retriable error
	hres, err := l.api.R().SetBody(&ledgerSignRequest{
		HDPath: hdpath,
		Msg:    msg,
	}).SetResult(&result).SetError(&errBody).Post("/sign")

	if err != nil {
		return
	}

	if hres.IsError() {
		err = fmt.Errorf("zap: %d %s", hres.StatusCode(), errBody.Error)
		return
	}

	return result.Sig, nil
}

func (l *LedgerAPI) SignTx(tx *solana.Transaction) (err error) {
	messageContent, err := tx.Message.MarshalBinary()
	if err != nil {
		return fmt.Errorf("unable to encode message for signing: %w", err)
	}

	m := tx.Message
	signerKeys := m.AccountKeys[0:m.Header.NumRequiredSignatures]
	// signerKeys := tx.Message.signerKeys()

	for _, key := range signerKeys {
		s, err := l.SignAs(key, messageContent)
		if err != nil {
			return fmt.Errorf("failed to signed with key %q: %w", key.String(), err)
		}

		tx.Signatures = append(tx.Signatures, s)
	}

	return nil
}

func (l *LedgerAPI) PrivateKey(pubkey solana.PublicKey) *solana.PrivateKey {
	return nil
}
