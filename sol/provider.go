package sol

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/sourcegraph/conc/iter"
	"go.uber.org/multierr"

	"github.com/rs/zerolog"
	"github.com/samber/lo"

	ag_binary "github.com/gagliardetto/binary"
)

type SendTxError struct {
	Err error
	Sig solana.Signature
}

var (
	ErrTxTimeout  = errors.New("tx confirm timeout")
	ErrTxTooLarge = errors.New("tx too large")
)

const SLOT_IN_YEAR = (60 * 60 * 24 * 365) * 2

const maxTxSize = 1232

func NewProvider(cfg ProviderConfig, signer Signer, client *rpc.Client, log *zerolog.Logger) *Provider {
	return &Provider{
		ProviderConfig: cfg,
		Ctx:            context.Background(),
		Signer:         signer,
		Client:         client,
		Log:            log,
	}
}

type ProviderConfig struct {
	Debug         bool
	Payer         solana.PublicKey
	Confirmations uint64
}

type Provider struct {
	ProviderConfig

	Ctx    context.Context
	Signer Signer
	Client *rpc.Client
	Log    *zerolog.Logger
}

func (pr *Provider) CurrentSlot() (uint64, error) {
	out, err := pr.Client.GetSlot(
		pr.Ctx,
		rpc.CommitmentFinalized,
	)
	return out, err
}

func (pr *Provider) WithConfirmations(confirms uint64) *Provider {
	// make a copy and change the required confirmations
	provider := *pr
	provider.Confirmations = confirms
	return &provider
}

func (pr *Provider) Lamports(account solana.PublicKey) (uint64, error) {
	acc, err := pr.Client.GetAccountInfo(pr.Ctx, account)
	if err != nil {
		if errors.Is(err, rpc.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return acc.Value.Lamports, nil
}

func (pr *Provider) HasLamports(account solana.PublicKey) (bool, error) {
	lamports, err := pr.Lamports(account)
	if err != nil {
		return false, err
	}

	return lamports > 0, nil
}

func (pr *Provider) HasMinLamports(account solana.PublicKey, minBalance uint64) (bool, error) {
	lamports, err := pr.Lamports(account)
	if err != nil {
		return false, err
	}

	return lamports >= minBalance, nil
}

// BatchInstructions group instructions together, without exceeding the size-limit for transactions
func (pr *Provider) BatchInstructions(ins []solana.Instruction) ([][]solana.Instruction, error) {
	client := pr.Client
	ctx := pr.Ctx

	hash, err := client.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, err
	}

	opts := solana.TransactionPayer(pr.Payer)

	var curtxins []solana.Instruction
	var alltxins [][]solana.Instruction

	var fakeSigner FakeSigner

	i := 0
	for {
		txins := append(curtxins, ins[i])

		tx, err := solana.NewTransaction(txins, hash.Value.Blockhash, opts)
		if err != nil {
			return nil, err
		}

		// FIXME: damn. there needs to be a way to "fake" this without actually
		// having to sign the damn thing
		//
		// i think that... one stupid hack is to use a fake signer where the private
		// keys are sha256 of the public key -.- . since we only need to ensure the
		// uniqnuess of the mapping from signers to signatures
		err = fakeSigner.SignTx(tx)

		// err = pr.Signer.SignTx(tx)

		if err != nil {
			return nil, err
		}

		bytes, err := tx.MarshalBinary()
		if err != nil {
			return nil, err
		}

		if len(bytes) > maxTxSize {
			if len(txins) == 1 {
				// just one instruction generates a transaction that's too big
				return nil, fmt.Errorf("tx size too big: %d/%d", len(bytes), maxTxSize)
			}

			alltxins = append(alltxins, curtxins)
			curtxins = nil
			// retry current index by creating a new tx batch
			continue
		}

		// look at next instruction, and try to append to the current tx batch
		curtxins = txins
		i++

		if i >= len(ins) {
			alltxins = append(alltxins, curtxins)
			break
		}
	}

	return alltxins, nil
}

func (pr *Provider) txSize(instrs []solana.Instruction) (int, error) {
	tx, err := solana.NewTransaction(instrs, solana.Hash{}, solana.TransactionPayer(pr.Payer))
	if err != nil {
		return 0, err
	}

	var fakeSigner FakeSigner

	err = fakeSigner.SignTx(tx)
	if err != nil {
		return 0, err
	}

	bytes, err := tx.MarshalBinary()
	return len(bytes), err
}

// instruction in same group should also in same transaction
type InstructionGroup []solana.Instruction

func tryChunk[T any](collection []T, splitToNextChunk func(element T, currStart, currIndex int) (bool, error)) ([][]T, error) {
	if splitToNextChunk == nil {
		return nil, errors.New("splitToNextChunk not provided")
	}
	result := make([][]T, 0)

	curr := make([]T, 0)
	start := 0
	for i := range collection {
		nextChunk, err := splitToNextChunk(collection[i], start, i)
		if err != nil {
			return nil, err
		}
		if nextChunk {
			result = append(result, curr)
			curr = []T{collection[i]}
			start = i
			continue
		}
		curr = append(curr, collection[i])
	}
	if len(curr) > 0 {
		result = append(result, curr)
	}
	return result, nil
}

// BatchGroupInstructions group instructions together, without exceeding the size-limit for transactions
func (pr *Provider) BatchGroupInstructions(groups []InstructionGroup) ([][]solana.Instruction, error) {
	chunks, err := tryChunk(groups, func(element InstructionGroup, currStart, currIndex int) (bool, error) {
		s := groups[currStart : currIndex+1]
		instrs := lo.Flatten(lo.Map(s, func(t InstructionGroup, _ int) []solana.Instruction { return t }))
		txSize, err := pr.txSize(instrs)
		if err != nil {
			return false, err
		}

		if txSize < maxTxSize {
			return false, nil
		}

		if len(s) == 1 { // just one group generates a transaction that's too big
			return false, ErrTxTooLarge
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	var batches [][]solana.Instruction
	for _, chunk := range chunks {
		var batch []solana.Instruction
		for _, group := range chunk {
			batch = append(batch, group...)
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

// deprecated
// func (pr *Provider) buildAllowedOpsInstructions(
// 	alloweds []solana.PublicKey,
// 	datas [][]byte,
// ) ([]solana.Instruction, error) {
// 	ctx := pr.Ctx
// 	client := pr.Client
// 	wallet := pr.Wallet

// 	var instrs []solana.Instruction

// 	for i := 0; i < len(alloweds); i++ {
// 		allowedInstrAccount := alloweds[i]
// 		data := datas[i]

// 		var delegatePK *solana.PublicKey
// 		if sk := wallet.GetDelegateKey(); sk != nil {
// 			delegatePK = sk.PublicKey().ToPointer()
// 		}

// 		instr, err := BuildExecuteAllowedInstruction(ctx, client, allowedInstrAccount, data, delegatePK)
// 		if err != nil {
// 			return nil, err
// 		}
// 		instrs = append(instrs, instr)
// 	}

// 	return instrs, nil
// }

// buildUnpreparedTx returns a transaction that's unsigned, and has no recent block hash. Call prepareTx on it.
func (pr *Provider) buildUnpreparedTx(ins []solana.Instruction) (*solana.Transaction, error) {
	var emptyBlockHash solana.Hash

	opts := solana.TransactionPayer(pr.Payer)
	tx, err := solana.NewTransaction(ins, emptyBlockHash, opts)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func (pr *Provider) BuildPreparedTx(ins ...solana.Instruction) (*solana.Transaction, error) {
	tx, err := pr.buildUnpreparedTx(ins)
	if err != nil {
		return nil, err
	}

	err = pr.prepareTx(tx)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// prepareTx updates the recent blockhash, and re-sign the tx
func (pr *Provider) prepareTx(tx *solana.Transaction) error {
	client := pr.Client

	ctx := pr.Ctx

	// damn how do i resign the tx?
	hash, err := client.GetRecentBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return err
	}

	tx.Message.RecentBlockhash = hash.Value.Blockhash

	if pr.Debug {
		pr.Log.Debug().Msg(tx.String())
	}

	// tx.Sign appends signatures. so we should clear the signatures before signing
	tx.Signatures = nil
	err = pr.Signer.SignTx(tx)
	if err != nil {
		return err
	}

	return nil
}

func (pr *Provider) sendAndConfirmTx(tx *solana.Transaction) (*solana.Signature, error) {
	client := pr.Client

	ctx := context.Background()

	sig, err := client.SendTransaction(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("send transaction failed: %w", err)
	}

	log := pr.Log.With().Str("sig", sig.String()).Logger()

	pr.Log.Debug().Msgf("https://solscan.io/tx/%s", sig)
	log.Debug().Msg("tx submitted")

	startTime := time.Now()
	confirmTimeout := 60 * time.Second
	finalizeTimeout := 3 * 60 * time.Second

	var confirmations uint64
	txStatus := "unknown"

	// 31 confirmation is finalized. The RPC returns nil for confirmation once
	// it's finalized, so the loop may exit even if the required confirmations are
	// not met (e.g. 27)
	var requiredConfirmations uint64 = 32
	if pr.ProviderConfig.Confirmations > 0 {
		requiredConfirmations = pr.ProviderConfig.Confirmations
	}

	for {
		time.Sleep(3 * time.Second)

		if confirmations == 0 && time.Since(startTime) > confirmTimeout {
			// FIXME: should implement an error struct
			log.Debug().Msg("confirm timeout")
			return nil, fmt.Errorf("tx: %s, %w", sig.String(), ErrTxTimeout)
		}

		if confirmations > 0 && time.Since(startTime) > finalizeTimeout {
			log.Debug().Msg("finalize timeout")
			return nil, errors.New("finalize timeout")
		}

		ss, err := client.GetSignatureStatuses(ctx, true, sig)
		if err != nil {
			log.Debug().Err(err).Msg("check tx status error")
			continue
		}

		// The returned value could be nil if the tx doesn't yet have any confirmation
		status := ss.Value[0]

		if status != nil {
			txStatus = string(status.ConfirmationStatus)

			if status.Confirmations != nil {
				// if finalized, this is nil. what a retarded API
				confirmations = *status.Confirmations
			}
		}

		log.Debug().Str("status", txStatus).Uint64("confirmations", confirmations).Msg("tx poll")

		if status == nil {
			// unconfirmed
			continue
		}

		if status.Confirmations == nil {
			// finalized or rooted
			break
		}

		if *status.Confirmations >= requiredConfirmations {
			// confirmed, but yet finalized
			break
		}
	}

	log.Debug().Msg("tx confirmed")

	return &sig, err
}

func (pr *Provider) IsTxFinalized(sig solana.Signature) (bool, error) {
	// TODO more options for confirm
	// - consider just use confirms?
	// - or use solana commitmentStatus?

	ctx := context.Background()
	ss, err := pr.Client.GetSignatureStatuses(ctx, true, sig)
	if err != nil {
		return false, err
	}

	if len(ss.Value) == 0 || ss.Value[0] == nil {
		return false, nil
	}

	finalized := ss.Value[0].ConfirmationStatus == rpc.ConfirmationStatusFinalized
	return finalized, nil
}

func (act *Provider) TokenAccountBalance(holder solana.PublicKey) (uint64, error) {
	ctx := act.Ctx

	ret, err := act.Client.GetTokenAccountBalance(ctx, holder, rpc.CommitmentConfirmed)

	if err != nil {
		if errors.Is(err, rpc.ErrNotFound) {
			return 0, nil
		}

		return 0, err
	}

	return strconv.ParseUint(ret.Value.Amount, 10, 64)
}

// return balances of given holders
// - for token account use token account amount as balance
// - for native account use lamports as balance
func (act *Provider) TokenAccounts(holders ...solana.PublicKey) ([]*TokenAccount, error) {
	tas, err := act.Client.GetMultipleAccounts(act.Ctx, holders...)
	if err != nil {
		return nil, err
	}

	ret := make([]*TokenAccount, 0)
	for i := 0; i < len(tas.Value); i++ {
		acc := tas.Value[i]
		if acc == nil {
			ret = append(ret, nil)
			continue
		}
		if !acc.Owner.Equals(solana.TokenProgramID) {
			// for native account, use SOL balance (Lamports)
			ret = append(ret, &TokenAccount{
				Address: holders[i],
				Account: token.Account{
					Mint:   solana.SolMint,
					Amount: acc.Lamports,
				},
			})
			continue
		}
		state, err := DecodeTokenAccount(acc.Data.GetBinary())
		if err != nil {
			return nil, err
		}
		ret = append(ret, &TokenAccount{
			Address: holders[i],
			Account: state,
		})
	}

	return ret, nil
}

func (act *Provider) BalanceOf(mint, owner solana.PublicKey) (uint64, error) {
	ata := act.ATA(mint, owner)

	tb, err := act.TokenAccountBalance(ata)

	if err != nil {
		if errors.Is(err, rpc.ErrNotFound) {
			return 0, nil
		}

		return 0, err
	}

	return tb, nil
}

func DecodeTokenAccount(data []byte) (ta token.Account, err error) {
	err = ta.UnmarshalWithDecoder(ag_binary.NewBinDecoder(data))
	return
}

type TokenAccount struct {
	Address solana.PublicKey
	token.Account
}

func (act *Provider) TokenAccount(addr solana.PublicKey) (ta TokenAccount, err error) {
	ret, err := act.Client.GetAccountInfo(act.Ctx, addr)
	if err != nil {
		return
	}

	dta, err := DecodeTokenAccount(ret.Value.Data.GetBinary())
	if err != nil {
		return
	}

	return TokenAccount{
		Address: addr,
		Account: dta,
	}, nil
}

func (act *Provider) BalancesOf(owner solana.PublicKey) (map[solana.PublicKey]uint64, error) {
	tas, err := act.TokenAccountsOf(owner)
	if err != nil {
		return nil, err
	}

	balances := make(map[solana.PublicKey]uint64)

	for _, ta := range tas {
		amount := balances[ta.Mint]
		balances[ta.Mint] = amount + ta.Amount
	}

	return balances, nil
}

func (act *Provider) TokenAccountsOf(owner solana.PublicKey) ([]TokenAccount, error) {
	tas, err := act.Client.GetTokenAccountsByOwner(act.Ctx, owner, &rpc.GetTokenAccountsConfig{
		ProgramId: &solana.TokenProgramID,
	}, &rpc.GetTokenAccountsOpts{})

	if err != nil {
		return nil, err
	}

	ret := make([]TokenAccount, 0)

	for i := 0; i < len(tas.Value); i++ {
		ta := tas.Value[i]
		state, err := DecodeTokenAccount(ta.Account.Data.GetBinary())
		if err != nil {
			return nil, err
		}

		ret = append(ret, TokenAccount{
			Address: ta.Pubkey,
			Account: state,
		})
	}
	return ret, nil
}

func (act *Provider) BalancesOfOwners(mint solana.PublicKey, owners ...solana.PublicKey) (map[solana.PublicKey]uint64, error) {
	atas := lo.Map(owners, func(owner solana.PublicKey, _ int) solana.PublicKey { return act.ATA(mint, owner) })
	ret, err := act.Client.GetMultipleAccounts(act.Ctx, atas...)
	if err != nil {
		return nil, nil
	}

	m := make(map[solana.PublicKey]uint64, len(owners))
	for i, acc := range ret.Value {
		if acc == nil {
			m[owners[i]] = 0
			continue
		}

		ta, err := DecodeTokenAccount(acc.Data.GetBinary())
		if err != nil {
			return m, err
		}
		m[owners[i]] = ta.Amount
	}
	return m, nil
}

func findATA(
	owner solana.PublicKey,
	mint solana.PublicKey,
) solana.PublicKey {
	add, _, err := solana.FindProgramAddress([][]byte{
		owner[:], token.ProgramID[:], mint[:],
	}, associatedtokenaccount.ProgramID)
	if err != nil {
		panic(err)
	}

	return add
}

func (pr *Provider) ATA(mint, owner solana.PublicKey) solana.PublicKey {
	return findATA(
		owner,
		mint,
	)
}

// Send sends instructions in batches of transactions. If error, returns the signature of the failed transaction.
func (pr *Provider) SendGroups(groups []InstructionGroup) error {
	if len(groups) == 0 {
		return nil
	}

	insBatches, err := pr.BatchGroupInstructions(groups)
	if err != nil {
		return err
	}

	return pr.sendBatches(insBatches)
}

func (pr *Provider) sendBatches(batchs [][]solana.Instruction) error {
	for i, ins := range batchs {
		pr.Log.Debug().Int("total", len(batchs)).Int("i", i).Msg("send batch")
		_, err := pr.SendInOneTx(ins)
		if err != nil {
			return err
		}
	}

	return nil
}

// Send sends instructions in batches of transactions. If error, returns the signature of the failed transaction.
func (pr *Provider) Send(ins ...solana.Instruction) error {
	if len(ins) == 0 {
		return nil
	}

	insBatches, err := pr.BatchInstructions(ins)
	if err != nil {
		return err
	}

	return pr.sendBatches(insBatches)
}

func (pr *Provider) SendParallel(concurrency int, ins ...solana.Instruction) error {
	insBatches, err := pr.BatchInstructions(ins)
	if err != nil {
		return err
	}

	errChan := make(chan error, len(insBatches))
	iter.ForEach(insBatches, func(ins *[]solana.Instruction) {
		_, err := pr.SendInOneTx(*ins)
		if err != nil {
			errChan <- err
		} else {
			errChan <- nil
		}
	})
	close(errChan)

	return multierr.Combine(lo.ChannelToSlice(errChan)...)
}

func (pr *Provider) SimulateOneTx(ins []solana.Instruction, accounts ...solana.PublicKey) (*rpc.SimulateTransactionResponse, error) {
	tx, err := pr.buildUnpreparedTx(ins)
	if err != nil {
		return nil, err
	}

	err = pr.prepareTx(tx)
	if err != nil {
		return nil, err
	}

	if pr.Debug {
		pr.Log.Debug().Msgf("simulate tx: %s", tx.String())
	}

	if accounts == nil {
		// the API borks if it's nil. Force it to be an empty array.
		accounts = make([]solana.PublicKey, 0)
	}

	out, err := pr.Client.SimulateTransactionWithOpts(pr.Ctx, tx, &rpc.SimulateTransactionOpts{
		Accounts: &rpc.SimulateTransactionAccountsOpts{
			Encoding:  solana.EncodingBase64,
			Addresses: accounts,
		},
	})

	if err != nil {
		return nil, err
	}

	if out.Value.Err != nil {
		pr.Log.Debug().Interface("logs", out.Value.Logs).Msg("simulate logs")
		return nil, fmt.Errorf("simulation failed: %v", out.Value.Err)
	}

	return out, nil
}

func (pr *Provider) SimulateWithBalancesChanges(ins []solana.Instruction, accounts ...solana.PublicKey) (map[solana.PublicKey]int64, error) {
	// WIP: we could find the token accounts from the instructions, but we will have a problem:
	//      the instruction include token accounts of both side, the user and the pool, so we might
	//      end up with mixed up balances since we use the token mint as key
	//      for example the user balance will be overwrite by the pool balance changes (same mint)
	//      we could use the ATA address as key, but then the caller of this SimulateWithBalancesChanges will need to
	//      find the ATA address, so we go back to having to find the atas
	// accounts, err := slice.TryMap(ins, func(in solana.Instruction) ([]solana.PublicKey, error) {
	// 	return slice.TryMap(in.Accounts(), func(a *solana.AccountMeta) (solana.PublicKey, error) {
	// 		if !a.IsWritable {
	// 			return solana.PublicKey{}, slice.Skip
	// 		}
	// 		return a.PublicKey, nil
	// 	})
	// })
	// if err != nil {
	// 	return nil, err
	// }
	// accounts = lo.Flatten(accounts)

	res, err := pr.SimulateOneTx(ins, accounts...)
	if err != nil {
		return nil, err
	}

	balances := map[solana.PublicKey]int64{}
	for _, acc := range res.Value.Accounts {
		if acc.Owner == solana.TokenProgramID {
			var tokenAcc token.Account
			err = ag_binary.NewBinDecoder(acc.Data.GetBinary()).Decode(&tokenAcc)
			if err != nil {
				return nil, err
			}
			// we get current balance and subtract the post tx balance
			curBal, err := pr.BalanceOf(tokenAcc.Mint, tokenAcc.Owner)
			if err != nil {
				return nil, err
			}
			// if negative is spending (out)
			balances[tokenAcc.Mint] = int64(tokenAcc.Amount) - int64(curBal)
		}
	}

	return balances, nil
}

func (pr *Provider) SendInOneTx(ins []solana.Instruction) (*solana.Signature, error) {
	tx, err := pr.buildUnpreparedTx(ins)
	if err != nil {
		return nil, fmt.Errorf("buildUnpreparedTx failed: %w", err)
	}

	return pr.SendTx(tx)
}

func (pr *Provider) SendTx(tx *solana.Transaction) (*solana.Signature, error) {
	for {
		err := pr.prepareTx(tx)
		if err != nil {
			return nil, err
		}

		// don't retry if if it's a simulation error
		sig, err := pr.sendAndConfirmTx(tx)

		if err != nil {
			if errors.Is(err, ErrTxTimeout) {
				continue
			}

			errStr := err.Error()

			// "Node is behind by 151 slots"
			if strings.Contains(errStr, "Node is behind by") {
				pr.Log.Info().Msg("Node is behind. Will resubmit tx in 30s")
				time.Sleep(30 * time.Second)
				continue
			}

			if strings.Contains(errStr, "Blockhash not found") {
				pr.Log.Info().Msg("Blockhash not found. Will resubmit in 5s")
				time.Sleep(5 * time.Second)
				continue
			}

			return nil, err
		}

		if err == nil {
			return sig, err
		}
	}
}

func (pr *Provider) TokenSupply(mint solana.PublicKey) (uint64, error) {
	iouMint, err := pr.Client.GetTokenSupply(pr.Ctx, mint, rpc.CommitmentFinalized)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(iouMint.Value.Amount, 10, 64)
}

func (pr *Provider) ExtendKey(sks ...solana.PrivateKey) *Provider {
	return pr.WithSigner(Extend(pr.Signer, sks...))
}

func (pr *Provider) WithSigner(newSigner Signer) *Provider {
	var newProvider = *pr // copy

	// extend the provider signer
	newProvider.Signer = newSigner

	return &newProvider
}

func (pr *Provider) Extend(extend Signer) *Provider {
	return pr.WithSigner(ExtendSigner(pr.Signer, extend))
}

// create ATA instruction if needed
func (pr *Provider) BuildCreateATAInstructionIfNeeded(
	ctx context.Context,
	payer, mint, owner solana.PublicKey,
) (solana.Instruction, error) {
	ata, _, _ := solana.FindAssociatedTokenAddress(owner, mint)
	_, err := pr.Client.GetAccountInfo(ctx, ata)
	if err == nil {
		return nil, nil
	}

	if !errors.Is(err, rpc.ErrNotFound) {
		return nil, err
	}

	return associatedtokenaccount.NewCreateInstruction(
		payer, owner, mint,
	).Build(), nil
}
