// Copyright (c) 2025 Pano Operations Ltd
//
// Use of this software is governed by the Business Source License included
// in the LICENSE file and at panoptisDev.com/bsl11.
//
// Change Date: 2028-4-16
//
// On the date above, in accordance with the Business Source License, use of
// this software will be governed by the GNU Lesser General Public License v3.

package floria

import (
	"fmt"
	"math/big"

	"github.com/panoptisDev/tosca/go/tosca"
	"github.com/holiman/uint256"
)

const (
	TxGas                     = 21_000
	TxGasContractCreation     = 53_000
	TxDataNonZeroGasEIP2028   = 16
	TxDataZeroGasEIP2028      = 4
	TxAccessListAddressGas    = 2400
	TxAccessListStorageKeyGas = 1900
	InitCodeWordGas           = 2

	createGasCostPerByte = 200
	maxCodeSize          = 24576
	maxInitCodeSize      = 2 * maxCodeSize

	BlobTxBlobGasPerBlob = 1 << 17 // Gas per blob, introduced in EIP-4844.

	MaxRecursiveDepth = 1024 // Maximum depth of call/create stack.
)

func init() {
	tosca.RegisterProcessorFactory("floria", newFloriaProcessor)
}

// newFloriaProcessor creates a new instance of the Floria processor with the given interpreter.
// This version of Floria is compatible with the Pano blockchain, but does not support Ethereum.
// There are 4 differences in the way transactions are handled:
// - Ignore gasFeeCap
// - Ignore value transfer in balance check on top level
// - No update of the coinbase
// - Consume 10% of the remaining gas
func newFloriaProcessor(interpreter tosca.Interpreter) tosca.Processor {
	return &Processor{
		Interpreter:   interpreter,
		EthCompatible: false,
	}
}

// Processor implements the tosca.Processor interface for the Floria processor.
type Processor struct {
	Interpreter   tosca.Interpreter
	EthCompatible bool
}

// Run checks whether the transaction can be executed and applies it if possible.
// It returns a receipt with the result of the transaction or an error in case the
// transaction can not be executed.
func (p *Processor) Run(
	blockParameters tosca.BlockParameters,
	transaction tosca.Transaction,
	context tosca.TransactionContext,
) (tosca.Receipt, error) {

	if err := checkTransaction(blockParameters, transaction, context); err != nil {
		return tosca.Receipt{}, err
	}

	gasPrice, gas, err := calculateAvailableGas(blockParameters, transaction, context, p.EthCompatible)
	if err != nil {
		return tosca.Receipt{}, err
	}

	result, err := p.runTransaction(blockParameters, transaction, context, gasPrice, gas)
	if err != nil {
		// An error here is due to an implementation bug or runtime error, the state has already been modified.
		return tosca.Receipt{GasUsed: transaction.GasLimit}, err
	}

	gasUsed := returnExcessGas(blockParameters, transaction, context, gasPrice, result, p.EthCompatible)

	receipt := tosca.Receipt{
		Success: result.Success,
		GasUsed: gasUsed,
		Output:  result.Output,
		Logs:    context.GetLogs(),
	}

	if transaction.Recipient == nil {
		receipt.ContractAddress = &result.CreatedAddress
	}

	return receipt, nil
}

// checkTransaction performs basic checks to ensure the validity of the transaction.
// It ensures the nonce is correct, the transaction is from an EOA, blobs are valid,
// and the init code size is not too large.
func checkTransaction(
	blockParameters tosca.BlockParameters,
	transaction tosca.Transaction,
	context tosca.TransactionContext,
) error {
	if err := nonceCheck(transaction.Nonce, context.GetNonce(transaction.Sender)); err != nil {
		return fmt.Errorf("failed nonce check: %w", err)
	}

	if err := eoaCheck(transaction.Sender, context.GetCodeHash(transaction.Sender)); err != nil {
		return fmt.Errorf("failed EOA check: %w", err)
	}

	if err := checkBlobs(transaction, blockParameters); err != nil {
		return fmt.Errorf("failed blob check: %w", err)
	}

	if err := initCodeSizeCheck(blockParameters.Revision, transaction); err != nil {
		return fmt.Errorf("failed init code size check: %w", err)
	}

	return nil
}

// calculateAvailableGas calculates the gasPrice and available gas for the transaction.
// An error is returned if the gasPrice can not be calculated, the sender does not have
// enough balance or the gasLimit is too low for the set up gas.
func calculateAvailableGas(
	blockParameters tosca.BlockParameters,
	transaction tosca.Transaction,
	context tosca.TransactionContext,
	ethCompatible bool,
) (tosca.Value, tosca.Gas, error) {
	gasPrice, err := calculateGasPrice(blockParameters.BaseFee, transaction.GasFeeCap, transaction.GasTipCap)
	if err != nil {
		return tosca.Value{}, 0, fmt.Errorf("failed to calculate gas price: %w", err)
	}

	if err = balanceCheck(gasPrice, transaction, context.GetBalance(transaction.Sender), ethCompatible); err != nil {
		return tosca.Value{}, 0, fmt.Errorf("failed balance check: %w", err)
	}

	setupGas := calculateSetupGas(transaction, blockParameters.Revision)
	if transaction.GasLimit < setupGas {
		return tosca.Value{}, transaction.GasLimit, fmt.Errorf("insufficient gas for set up")
	}

	buyGasInternal(transaction, gasPrice, blockParameters.BlobBaseFee, context)
	gas := transaction.GasLimit - setupGas

	return gasPrice, gas, nil
}

// runTransaction executes the transaction and returns the result.
// Non executable transactions return a result with success marked as false,
// errors are implementation bugs or runtime errors.
func (p *Processor) runTransaction(
	blockParameters tosca.BlockParameters,
	transaction tosca.Transaction,
	context tosca.TransactionContext,
	gasPrice tosca.Value,
	gas tosca.Gas) (tosca.CallResult, error) {

	transactionParameters := tosca.TransactionParameters{
		Origin:     transaction.Sender,
		GasPrice:   gasPrice,
		BlobHashes: transaction.BlobHashes,
	}

	runContext := runContext{
		floriaContext{context},
		p.Interpreter,
		blockParameters,
		transactionParameters,
		0,
		false,
	}

	if blockParameters.Revision >= tosca.R09_Berlin {
		setUpAccessList(transaction, &runContext, blockParameters.Revision, blockParameters.Coinbase)
	}

	callParameters := callParameters(transaction, gas)
	kind := callKind(transaction)

	if kind == tosca.Call {
		context.SetNonce(transaction.Sender, context.GetNonce(transaction.Sender)+1)
	}

	return runContext.Call(kind, callParameters)
}

// returnExcessGas returns the excess gas back to the sender.
// The ethereum compatible version transfers the tip to the coinbase.
func returnExcessGas(
	blockParameters tosca.BlockParameters,
	transaction tosca.Transaction,
	context tosca.TransactionContext,
	gasPrice tosca.Value,
	result tosca.CallResult,
	ethCompatible bool,
) tosca.Gas {
	gasLeft := calculateGasLeft(transaction, result, blockParameters.Revision, ethCompatible)
	refundGasInternal(context, transaction.Sender, gasPrice, gasLeft)

	gasUsed := transaction.GasLimit - gasLeft
	if ethCompatible {
		paymentToCoinbase(gasPrice, gasUsed, blockParameters, context)
	}

	return gasUsed
}

// calculateGasPrice calculates the gas price of the transaction considering the tip.
// The calculation fails if the maximum price is smaller than the base price or if the
// maximum price exceeds the maximum tip.
func calculateGasPrice(baseFee, gasFeeCap, gasTipCap tosca.Value) (tosca.Value, error) {
	if gasFeeCap.Cmp(baseFee) < 0 {
		return tosca.Value{}, fmt.Errorf("gasFeeCap %v is lower than baseFee %v", gasFeeCap, baseFee)
	}
	if gasFeeCap.Cmp(gasTipCap) < 0 {
		return tosca.Value{}, fmt.Errorf("gasFeeCap %v is lower than tipCap %v", gasFeeCap, gasTipCap)
	}
	return tosca.Add(baseFee, tosca.Min(gasTipCap, tosca.Sub(gasFeeCap, baseFee))), nil
}

func nonceCheck(transactionNonce uint64, stateNonce uint64) error {
	if transactionNonce != stateNonce {
		return fmt.Errorf("nonce mismatch: %v != %v", transactionNonce, stateNonce)
	}
	if stateNonce+1 < stateNonce {
		return fmt.Errorf("nonce overflow")
	}
	return nil
}

// eoaCheck ensures that the sender is an externally owned account (EOA),
// transactions from contracts are not allowed.
func eoaCheck(sender tosca.Address, codeHash tosca.Hash) error {
	if codeHash != (tosca.Hash{}) && codeHash != emptyCodeHash {
		return fmt.Errorf("sender is not an EOA")
	}
	return nil
}

// balanceCheck checks if the sender has enough balance to cover for the transaction gas limit and value.
func balanceCheck(gasPrice tosca.Value, transaction tosca.Transaction, balance tosca.Value, ethCompatible bool) error {
	checkValue := gasPrice.ToBig().Mul(gasPrice.ToBig(), big.NewInt(int64(transaction.GasLimit)))
	if ethCompatible && transaction.GasFeeCap != (tosca.Value{}) {
		checkValue = transaction.GasFeeCap.ToBig().Mul(transaction.GasFeeCap.ToBig(), big.NewInt(int64(transaction.GasLimit)))
	}

	if ethCompatible {
		// Note: insufficient balance for **topmost** call isn't a consensus error in Opera, unlike Ethereum
		// Such transaction will revert and consume sender's gas
		checkValue = checkValue.Add(checkValue, transaction.Value.ToBig())
	}

	if len(transaction.BlobHashes) > 0 {
		blobFee := transaction.BlobGasFeeCap.Scale(uint64(len(transaction.BlobHashes) * BlobTxBlobGasPerBlob))
		checkValue = checkValue.Add(checkValue, blobFee.ToBig())
	}

	capGasU256, overflow := uint256.FromBig(checkValue)
	if overflow {
		return fmt.Errorf("capGas overflow")
	}
	capGasValue := tosca.ValueFromUint256(capGasU256)

	if balance.Cmp(capGasValue) < 0 {
		return fmt.Errorf("insufficient balance: %v < %v", balance, capGasValue)
	}

	return nil
}

// initCodeSizeCheck ensures the init code size for contract creation is smaller than the maximum.
func initCodeSizeCheck(revision tosca.Revision, transaction tosca.Transaction) error {
	if revision >= tosca.R12_Shanghai && transaction.Recipient == nil &&
		len(transaction.Input) > maxInitCodeSize {
		return fmt.Errorf("init code too long")
	}
	return nil
}

// Decreases the sender balance by the transaction gas limit.
// This function does not check for sufficient balance and requires the balance check to be performed in advance.
func buyGasInternal(transaction tosca.Transaction, gasPrice tosca.Value, blobGasPrice tosca.Value, context tosca.TransactionContext) {
	gas := gasPrice.Scale(uint64(transaction.GasLimit))

	if len(transaction.BlobHashes) > 0 {
		blobFee := blobGasPrice.Scale(uint64(len(transaction.BlobHashes) * BlobTxBlobGasPerBlob))
		gas = tosca.Add(gas, blobFee)
	}

	senderBalance := context.GetBalance(transaction.Sender)
	senderBalance = tosca.Sub(senderBalance, gas)
	context.SetBalance(transaction.Sender, senderBalance)
}

// setUpAccessList sets up the access list for the transaction by adding accounts and storage keys.
func setUpAccessList(transaction tosca.Transaction, context tosca.TransactionContext, revision tosca.Revision, coinBase tosca.Address) {
	if transaction.AccessList == nil {
		return
	}

	context.AccessAccount(transaction.Sender)
	if transaction.Recipient != nil {
		context.AccessAccount(*transaction.Recipient)
	}

	precompiles := getPrecompiledAddresses(revision)
	for _, address := range precompiles {
		context.AccessAccount(address)
	}

	for _, accessTuple := range transaction.AccessList {
		context.AccessAccount(accessTuple.Address)
		for _, key := range accessTuple.Keys {
			context.AccessStorage(accessTuple.Address, key)
		}
	}

	if revision >= tosca.R12_Shanghai {
		context.AccessAccount(coinBase)
	}
}

// callKind determines whether the transaction is a call or a create.
func callKind(transaction tosca.Transaction) tosca.CallKind {
	if transaction.Recipient == nil {
		return tosca.Create
	}
	return tosca.Call
}

// callParameters extracts the call parameters from the transaction.
func callParameters(transaction tosca.Transaction, gas tosca.Gas) tosca.CallParameters {
	callParameters := tosca.CallParameters{
		Sender: transaction.Sender,
		Input:  transaction.Input,
		Value:  transaction.Value,
		Gas:    gas,
	}
	if transaction.Recipient != nil {
		callParameters.Recipient = *transaction.Recipient
		callParameters.CodeAddress = *transaction.Recipient
	}
	return callParameters
}

// calculateGasLeft calculates the remaining gas after the transaction execution.
// The non ethereum compatible version consumes 10% of the remaining gas.
func calculateGasLeft(transaction tosca.Transaction, result tosca.CallResult, revision tosca.Revision, ethCompatible bool) tosca.Gas {
	gasLeft := result.GasLeft

	// 10% of remaining gas is charged for non-internal transactions
	if !ethCompatible && transaction.Sender != (tosca.Address{}) {
		gasLeft -= gasLeft / 10
	}

	if result.Success {
		gasUsed := transaction.GasLimit - gasLeft
		refund := result.GasRefund

		maxRefund := tosca.Gas(0)
		if revision < tosca.R10_London {
			// Before EIP-3529: refunds were capped to gasUsed / 2
			maxRefund = gasUsed / 2
		} else {
			// After EIP-3529: refunds are capped to gasUsed / 5
			maxRefund = gasUsed / 5
		}

		if refund > maxRefund {
			refund = maxRefund
		}
		gasLeft += refund
	}

	return gasLeft
}

// refundGasInternal transfers the remaining gas back to the sender.
func refundGasInternal(context tosca.TransactionContext, sender tosca.Address, gasPrice tosca.Value, gasLeft tosca.Gas) {
	refundValue := gasPrice.Scale(uint64(gasLeft))
	senderBalance := context.GetBalance(sender)
	senderBalance = tosca.Add(senderBalance, refundValue)
	context.SetBalance(sender, senderBalance)
}

// calculateSetupGas calculates the gas required for setting up the transaction.
// This includes costs for call or create, the input data and the access list.
func calculateSetupGas(transaction tosca.Transaction, revision tosca.Revision) tosca.Gas {
	var gas tosca.Gas
	if transaction.Recipient == nil {
		gas = TxGasContractCreation
	} else {
		gas = TxGas
	}

	if len(transaction.Input) > 0 {
		nonZeroBytes := tosca.Gas(0)
		for _, inputByte := range transaction.Input {
			if inputByte != 0 {
				nonZeroBytes++
			}
		}
		zeroBytes := tosca.Gas(len(transaction.Input)) - nonZeroBytes

		// No overflow check for the gas computation is required although it is performed in the
		// opera version. The overflow check would be triggered in a worst case with an input
		// greater than 2^64 / 16 - 53000 = ~10^18, which is not possible with real world hardware
		gas += zeroBytes * TxDataZeroGasEIP2028
		gas += nonZeroBytes * TxDataNonZeroGasEIP2028

		if transaction.Recipient == nil && revision >= tosca.R12_Shanghai {
			lenWords := tosca.SizeInWords(uint64(len(transaction.Input)))
			gas += tosca.Gas(lenWords * InitCodeWordGas)
		}
	}

	if transaction.AccessList != nil {
		gas += tosca.Gas(len(transaction.AccessList)) * TxAccessListAddressGas

		// charge for each storage key
		for _, accessTuple := range transaction.AccessList {
			gas += tosca.Gas(len(accessTuple.Keys)) * TxAccessListStorageKeyGas
		}
	}

	return tosca.Gas(gas)
}

// paymentToCoinbase transfers the tip to the coinbase, only applicable for ethereum compatible version
func paymentToCoinbase(gasPrice tosca.Value, gasUsed tosca.Gas, blockParameters tosca.BlockParameters, context tosca.TransactionContext) {
	effectiveTip := gasPrice
	if blockParameters.Revision >= tosca.R10_London {
		effectiveTip = tosca.Sub(gasPrice, blockParameters.BaseFee)
	}
	fee := effectiveTip.Scale(uint64(gasUsed))
	context.SetBalance(blockParameters.Coinbase, tosca.Add(context.GetBalance(blockParameters.Coinbase), fee))
}

// checkBlobs validates the blob hashes and blob gas price of the transaction.
func checkBlobs(transaction tosca.Transaction, blockParameters tosca.BlockParameters) error {
	if transaction.BlobHashes != nil {
		if transaction.Recipient == nil {
			return fmt.Errorf("blob transaction without recipient")
		}
		if len(transaction.BlobHashes) == 0 {
			return fmt.Errorf("missing blob hashes")
		}
		for _, hash := range transaction.BlobHashes {
			// Perform kzg4844 valid version check
			if len(hash) != 32 || hash[0] != 0x01 {
				return fmt.Errorf("blob with invalid hash version")
			}
		}
	}

	if blockParameters.Revision >= tosca.R13_Cancun && len(transaction.BlobHashes) > 0 {
		if transaction.BlobGasFeeCap.Cmp(blockParameters.BlobBaseFee) < 0 {
			return fmt.Errorf("blobGasFeeCap is lower than blobBaseFee")
		}
	}
	return nil
}
