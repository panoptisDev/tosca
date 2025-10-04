// Copyright (c) 2025 Pano Operations Ltd
//
// Use of this software is governed by the Business Source License included
// in the LICENSE file and at panoptisDev.com/bsl11.
//
// Change Date: 2028-4-16
//
// On the date above, in accordance with the Business Source License, use of
// this software will be governed by the GNU Lesser General Public License v3.

package tosca

//go:generate mockgen -source processor.go -destination processor_mock.go -package tosca

// Processor is an interface for a component capable of executing transactions.
// Implementations are executing individual transactions to progress the world state
// of a chain. In particular, they handle the charging of gas fees, the checking of
// nonces, the execution of transactions using (potentially) recursive calls of contracts,
// the integration of precompiled contracts, and the creation of new contracts.
type Processor interface {
	// Run executes the transaction provided by the parameters in the specified context.
	Run(BlockParameters, Transaction, TransactionContext) (Receipt, error)
}

// Transaction summarizes the parameters of a transaction to be executed on a chain.
type Transaction struct {
	Sender            Address       // the sender of the transaction, paying for its execution
	Recipient         *Address      // the receiver of a transaction, nil if a new contract is to be created
	Nonce             uint64        // the nonce of the sender account, used to prevent replay attacks
	Input             Data          // the input data for the transaction
	Value             Value         // the amount of network currency to transfer to the recipient
	GasLimit          Gas           // the maximum amount of gas that can be used by the transaction
	GasFeeCap         Value         // the amount of network currency the sender is willing to pay for one gas unit
	GasTipCap         Value         // the amount of network currency the sender is willing to pay for one unit as a priority fee
	BlobGasFeeCap     Value         // the amount of network currency the sender is willing to pay for blob gas
	BlobHashes        []Hash        // the hashes of the blobs for this transaction
	AccessList        []AccessTuple // the list of accounts and storage slots expected to be accessed
	AuthorizationList []SetCodeAuthorization
}

// SetCodeAuthorization contains the information required for EIP-7702 set code transactions to
// enable externally owned accounts (EOAs) to reference code and execute within their own context.
type SetCodeAuthorization struct {
	ChainID Word    // the chain ID for which this authorization is valid
	Address Address // the target address of the delegation
	Nonce   uint64  // the nonce of the signer, used to prevent replay
	V       uint8   // the recovery id of the signature
	R       Word    // the first value of the signature
	S       Word    // the second value of the signature
}

// AccessTuple lists a range of accounts and storage slots expected to be accessed
// by a transaction. Those are intended as hints for the actual access pattern. However,
// transactions are not required to provide those, nor can completeness and/or correctness
// be assumed.
type AccessTuple struct {
	Address Address
	Keys    []Key
}

// Receipt summarizes the result of the execution of a transaction.
type Receipt struct {
	Success         bool     // false if the execution ended in a revert, true otherwise
	Output          Data     // the output produced by the transaction
	ContractAddress *Address // filled if a contract was created by this transaction
	GasUsed         Gas      // gas used by contract calls
	BlobGasUsed     Gas      // gas used for blob transactions
	Logs            []Log    // logs produced by the transaction
}
