package bevm

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"go.dedis.ch/cothority/v3/byzcoin"
	"go.dedis.ch/cothority/v3/darc"
	"go.dedis.ch/onet/v3/log"
	"go.dedis.ch/protobuf"
	"golang.org/x/xerrors"
)

// WeiPerEther represents the number of Wei (the smallest currency denomination
// in Ethereum) in a single Ether.
const WeiPerEther = 1e18

// ---------------------------------------------------------------------------

// EvmContract is the abstraction for an Ethereum contract
type EvmContract struct {
	Abi      abi.ABI
	Bytecode []byte
	name     string // For informational purposes only
}

// EvmContractInstance is a deployed instance of an EvmContract
type EvmContractInstance struct {
	Parent  *EvmContract // The contract of which this is an instance
	Address common.Address
}

// NewEvmContract creates a new EvmContract and stores its ABI and bytecode.
func NewEvmContract(name string, abiJSON string, binData string) (
	*EvmContract, error) {
	contractAbi, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, xerrors.Errorf("failed to decode JSON for "+
			"contract ABI: %v", err)
	}

	contractBytecode := common.Hex2Bytes(binData)

	return &EvmContract{
		name:     name,
		Abi:      contractAbi,
		Bytecode: contractBytecode,
	}, nil
}

func (contract EvmContract) String() string {
	return fmt.Sprintf("EvmContract[%s]", contract.name)
}

func (contract EvmContract) packConstructor(args ...interface{}) (
	[]byte, error) {
	return contract.Abi.Pack("", args...)
}

func (contractInstance EvmContractInstance) String() string {
	return fmt.Sprintf("EvmContractInstance[%s @%s]",
		contractInstance.Parent.name, contractInstance.Address.Hex())
}

func (contractInstance EvmContractInstance) packMethod(method string,
	args ...interface{}) ([]byte, error) {
	return contractInstance.Parent.Abi.Pack(method, args...)
}

func (contractInstance EvmContractInstance) unpackResult(method string,
	resultBytes []byte) (interface{}, error) {
	methodAbi, ok := contractInstance.Parent.Abi.Methods[method]
	if !ok {
		return nil, xerrors.Errorf("method \"%s\" does not exist for "+
			"this contract", method)
	}

	abiOutputs := methodAbi.Outputs
	switch len(abiOutputs) {
	case 0:
		return nil, nil

	case 1:
		switch abiOutputs[0].Type.String() {
		case "uint256":
			// Solidity's uint256 is BigInt in the EVM
			result := big.NewInt(0)
			err := contractInstance.Parent.Abi.Unpack(&result, method,
				resultBytes)
			if err != nil {
				return nil, xerrors.Errorf("failed to unpack result "+
					"from EVM: %v", err)
			}

			return result, nil
		default:
			return nil, xerrors.Errorf("unsupported result type: %s",
				abiOutputs[0].Type)
		}

	default:
		return nil, xerrors.New("tuple return values not supported")
	}
}

// ---------------------------------------------------------------------------

// EvmAccount is the abstraction for an Ethereum account
type EvmAccount struct {
	Address    common.Address
	PrivateKey *ecdsa.PrivateKey
	Nonce      uint64
}

// NewEvmAccount creates a new EvmAccount
func NewEvmAccount(privateKey string) (*EvmAccount, error) {
	privKey, err := crypto.HexToECDSA(privateKey)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode private "+
			"key for account creation: %v", err)
	}

	address := crypto.PubkeyToAddress(privKey.PublicKey)

	return &EvmAccount{
		Address:    address,
		PrivateKey: privKey,
	}, nil
}

func (account EvmAccount) String() string {
	return fmt.Sprintf("EvmAccount[%s]", account.Address.Hex())
}

// ---------------------------------------------------------------------------

// Client is the abstraction for the ByzCoin EVM client
type Client struct {
	bcClient   *byzcoin.Client
	signer     darc.Signer
	instanceID byzcoin.InstanceID
}

// NewBEvm creates a new ByzCoin EVM instance
func NewBEvm(bcClient *byzcoin.Client, signer darc.Signer, gDarc *darc.Darc) (
	byzcoin.InstanceID, error) {
	instanceID := byzcoin.NewInstanceID(nil)

	tx, err := spawnBEvm(bcClient, signer,
		byzcoin.NewInstanceID(gDarc.GetBaseID()), &byzcoin.Spawn{
			ContractID: ContractBEvmID,
			Args:       byzcoin.Arguments{},
		})
	if err != nil {
		return instanceID, xerrors.Errorf("failed to execute ByzCoin "+
			"spawn command for BEvm contract: %v", err)
	}

	instanceID = tx.Instructions[0].DeriveID("")

	return instanceID, nil
}

// NewClient creates a new ByzCoin EVM client, connected to the given ByzCoin
// instance
func NewClient(bcClient *byzcoin.Client, signer darc.Signer,
	instanceID byzcoin.InstanceID) (*Client, error) {
	return &Client{
		bcClient:   bcClient,
		signer:     signer,
		instanceID: instanceID,
	}, nil
}

// Delete deletes the ByzCoin EVM client and all its state
func (client *Client) Delete() error {
	_, err := client.deleteBEvm(&byzcoin.Delete{
		ContractID: ContractBEvmID,
	})
	if err != nil {
		return xerrors.Errorf("failed to execute ByzCoin "+
			"delete command for BEvm instance: %v", err)
	}

	return nil
}

// Deploy deploys a new Ethereum contract on the EVM
func (client *Client) Deploy(gasLimit uint64, gasPrice uint64, amount uint64,
	account *EvmAccount, contract *EvmContract, args ...interface{}) (
	*EvmContractInstance, error) {
	log.Lvlf2(">>> Deploy EVM contract '%s'", contract)
	defer log.Lvlf2("<<< Deploy EVM contract '%s'", contract)

	packedArgs, err := contract.packConstructor(args...)
	if err != nil {
		return nil, xerrors.Errorf("failed to pack arguments for "+
			"contract constructor: %v", err)
	}

	callData := append(contract.Bytecode, packedArgs...)
	tx := types.NewContractCreation(account.Nonce, big.NewInt(int64(amount)),
		gasLimit, big.NewInt(int64(gasPrice)), callData)
	signedTxBuffer, err := account.signAndMarshalTx(tx)
	if err != nil {
		return nil, xerrors.Errorf("failed to prepare EVM transaction for "+
			"contract deployment: %v", err)
	}

	err = client.invoke("transaction", byzcoin.Arguments{
		{Name: "tx", Value: signedTxBuffer},
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to invoke ByzCoin transaction for "+
			"EVM contract deployment: %v", err)
	}

	contractInstance := &EvmContractInstance{
		Parent:  contract,
		Address: crypto.CreateAddress(account.Address, account.Nonce),
	}

	account.Nonce++

	return contractInstance, nil
}

// Transaction performs a new transaction (contract method call with state
// change) on the EVM
func (client *Client) Transaction(gasLimit uint64, gasPrice uint64,
	amount uint64, account *EvmAccount, contractInstance *EvmContractInstance,
	method string, args ...interface{}) error {
	log.Lvlf2(">>> EVM method '%s()' on %s", method, contractInstance)
	defer log.Lvlf2("<<< EVM method '%s()' on %s", method, contractInstance)

	callData, err := contractInstance.packMethod(method, args...)
	if err != nil {
		return xerrors.Errorf("failed to pack arguments for contract "+
			"method '%s': %v", method, err)
	}

	tx := types.NewTransaction(account.Nonce, contractInstance.Address,
		big.NewInt(int64(amount)), gasLimit, big.NewInt(int64(gasPrice)),
		callData)
	signedTxBuffer, err := account.signAndMarshalTx(tx)
	if err != nil {
		return xerrors.Errorf("failed to prepare EVM transaction for "+
			"method execution: %v", err)
	}

	err = client.invoke("transaction", byzcoin.Arguments{
		{Name: "tx", Value: signedTxBuffer},
	})
	if err != nil {
		return xerrors.Errorf("failed to invoke ByzCoin transaction for "+
			"EVM method execution: %v", err)
	}

	account.Nonce++

	return nil
}

// Call performs a new call (contract view method call, without state change)
// on the EVM
func (client *Client) Call(account *EvmAccount,
	contractInstance *EvmContractInstance,
	method string, args ...interface{}) (interface{}, error) {
	log.Lvlf2(">>> EVM view method '%s()' on %s", method, contractInstance)
	defer log.Lvlf2("<<< EVM view method '%s()' on %s",
		method, contractInstance)

	// Pack the method call and arguments
	callData, err := contractInstance.packMethod(method, args...)
	if err != nil {
		return nil, xerrors.Errorf("failed to pack arguments for contract "+
			"view method '%s': %v", method, err)
	}

	// Retrieve the EVM state
	stateDb, err := getEvmDb(client.bcClient, client.instanceID)
	if err != nil {
		return nil, xerrors.Errorf("failed to retrieve EVM state: %v", err)
	}

	// Instantiate a new EVM
	evm := vm.NewEVM(getContext(), stateDb, getChainConfig(), getVMConfig())

	// Perform the call (1 Ether should be enough for everyone [tm]...)
	ret, _, err := evm.Call(vm.AccountRef(account.Address),
		contractInstance.Address, callData, uint64(1*WeiPerEther),
		big.NewInt(0))
	if err != nil {
		return nil, xerrors.Errorf("failed to executing EVM view "+
			"method: %v", err)
	}

	// Unpack the result into the caller's variable
	result, err := contractInstance.unpackResult(method, ret)
	if err != nil {
		return nil, xerrors.Errorf("failed to unpack EVM view "+
			"method result: %v", err)
	}

	return result, nil
}

// CreditAccount credits the given Ethereum address with the given amount
func (client *Client) CreditAccount(amount *big.Int,
	address common.Address) error {
	err := client.invoke("credit", byzcoin.Arguments{
		{Name: "address", Value: address.Bytes()},
		{Name: "amount", Value: amount.Bytes()},
	})
	if err != nil {
		return xerrors.Errorf("failed to credit EVM account: %v", err)
	}

	log.Lvlf2("Credited %d wei on '%x'", amount, address)

	return nil
}

// GetAccountBalance returns the current balance of a Ethereum address
func (client *Client) GetAccountBalance(address common.Address) (
	*big.Int, error) {
	stateDb, err := getEvmDb(client.bcClient, client.instanceID)
	if err != nil {
		return nil, xerrors.Errorf("failed toretrieve EVM state: %v", err)
	}

	balance := stateDb.GetBalance(address)

	log.Lvlf2("Balance of '%x' is %d wei", address, balance)

	return balance, nil
}

// ---------------------------------------------------------------------------
// Helper functions

// signAndMarshalTx signs an Ethereum transaction and returns it in byte
// format, ready to be included into a Byzcoin transaction
func (account EvmAccount) signAndMarshalTx(tx *types.Transaction) (
	[]byte, error) {
	var signer types.Signer = types.HomesteadSigner{}

	signedTx, err := types.SignTx(tx, signer, account.PrivateKey)
	if err != nil {
		return nil, xerrors.Errorf("failed to sign EVM transaction: %v", err)
	}

	signedBuffer, err := signedTx.MarshalJSON()
	if err != nil {
		return nil, xerrors.Errorf("failed to serialize EVM transaction "+
			"to JSON: %v", err)
	}

	return signedBuffer, nil
}

// Retrieve a read-only EVM state database from ByzCoin
func getEvmDb(bcClient *byzcoin.Client, instID byzcoin.InstanceID) (
	*state.StateDB, error) {
	// Retrieve the proof of the Byzcoin instance
	proofResponse, err := bcClient.GetProof(instID[:])
	if err != nil {
		return nil, xerrors.Errorf("failed to retrieve BEvm instance: %v", err)
	}

	// Validate the proof
	err = proofResponse.Proof.Verify(bcClient.ID)
	if err != nil {
		return nil, xerrors.Errorf("failed to verify BEvm instance "+
			"proof: %v", err)
	}

	// Extract the value from the proof
	_, value, _, _, err := proofResponse.Proof.KeyValue()
	if err != nil {
		return nil, xerrors.Errorf("failed to get BEvm instance value: %v", err)
	}

	// Decode the proof value into an EVM State
	var bs State
	err = protobuf.Decode(value, &bs)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode BEvm instance "+
			"value: %v", err)
	}

	// Create a client ByzDB instance
	byzDb, err := NewClientByzDatabase(instID, bcClient)
	if err != nil {
		return nil, xerrors.Errorf("failed to creatw a new ByzDB "+
			"instance: %v", err)
	}

	db := state.NewDatabase(byzDb)

	return state.New(bs.RootHash, db)
}

// Invoke a method on a ByzCoin EVM instance
func (client *Client) invoke(command string, args byzcoin.Arguments) error {
	_, err := client.invokeBEvm(&byzcoin.Invoke{
		ContractID: ContractBEvmID,
		Command:    command,
		Args:       args,
	})
	if err != nil {
		return xerrors.Errorf("failed to execute ByzCoin invoke "+
			"instruction: %v", err)
	}

	return nil
}

func spawnBEvm(bcClient *byzcoin.Client, signer darc.Signer,
	instanceID byzcoin.InstanceID, instr *byzcoin.Spawn) (
	*byzcoin.ClientTransaction, error) {
	return execByzCoinTx(bcClient, signer, instanceID, instr, nil, nil)
}

func (client *Client) invokeBEvm(instr *byzcoin.Invoke) (
	*byzcoin.ClientTransaction, error) {
	return execByzCoinTx(client.bcClient, client.signer, client.instanceID,
		nil, instr, nil)
}

func (client *Client) deleteBEvm(instr *byzcoin.Delete) (
	*byzcoin.ClientTransaction, error) {
	return execByzCoinTx(client.bcClient, client.signer, client.instanceID,
		nil, nil, instr)
}

func execByzCoinTx(bcClient *byzcoin.Client,
	signer darc.Signer, instanceID byzcoin.InstanceID,
	spawnInstr *byzcoin.Spawn, invokeInstr *byzcoin.Invoke,
	deleteInstr *byzcoin.Delete) (*byzcoin.ClientTransaction, error) {
	counters, err := bcClient.GetSignerCounters(signer.Identity().String())
	if err != nil {
		return nil, xerrors.Errorf("failed to retrieve signer "+
			"counters from ByzCoin: %v", err)
	}

	tx, err := bcClient.CreateTransaction(byzcoin.Instruction{
		InstanceID:    instanceID,
		SignerCounter: []uint64{counters.Counters[0] + 1},
		Spawn:         spawnInstr,
		Invoke:        invokeInstr,
		Delete:        deleteInstr,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to create ByzCoin "+
			"transaction: %v", err)
	}

	err = tx.FillSignersAndSignWith(signer)
	if err != nil {
		return nil, xerrors.Errorf("failed to sign ByzCoin "+
			"transaction: %v", err)
	}

	// Sending this transaction to ByzCoin does not directly include it in the
	// global state - first we must wait for the new block to be created.
	_, err = bcClient.AddTransactionAndWait(tx, 5)
	if err != nil {
		return nil, xerrors.Errorf("failed to send ByzCoin "+
			"transaction: %v", err)
	}

	return &tx, nil
}
