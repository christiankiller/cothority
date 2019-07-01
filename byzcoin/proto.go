package byzcoin

import (
	"time"

	"go.dedis.ch/cothority/v3/byzcoin/trie"
	"go.dedis.ch/cothority/v3/darc"
	"go.dedis.ch/cothority/v3/skipchain"
	"go.dedis.ch/onet/v3"
)

// PROTOSTART
// package byzcoin;
// type :skipchain.SkipBlockID:bytes
// type :darc.ID:bytes
// type :darc.Action:string
// type :Arguments:[]Argument
// type :Instructions:[]Instruction
// type :TxResults:[]TxResult
// type :InstanceID:bytes
// type :Version:sint32
// import "skipchain.proto";
// import "onet.proto";
// import "darc.proto";
// import "trie.proto";
//
// option java_package = "ch.epfl.dedis.lib.proto";
// option java_outer_classname = "ByzCoinProto";

// GetAllChainIDsRequest is a request to get all the Byzcoin chains from a server.
type GetAllChainIDsRequest struct {
}

// GetAllChainIDsResponse contains the list of Byzcoin chains known by a server.
type GetAllChainIDsResponse struct {
	IDs []skipchain.SkipBlockID
}

// DataHeader is the data passed to the Skipchain
type DataHeader struct {
	// TrieRoot is the root of the merkle tree of the colleciton after
	// applying the valid transactions.
	TrieRoot []byte
	// ClientTransactionHash is the sha256 hash of all the transactions in the body
	ClientTransactionHash []byte
	// StateChangesHash is the sha256 of all the StateChanges generated by the
	// accepted transactions.
	StateChangesHash []byte
	// Timestamp is a Unix timestamp in nanoseconds.
	Timestamp int64
}

// DataBody is stored in the body of the skipblock, and it's hash is stored
// in the DataHeader.
type DataBody struct {
	TxResults TxResults
}

// ***
// These are the messages used in the API-calls
// ***

// CreateGenesisBlock asks the cisc-service to set up a new skipchain.
type CreateGenesisBlock struct {
	// Version of the protocol
	Version Version
	// Roster defines which nodes participate in the skipchain.
	Roster onet.Roster
	// GenesisDarc defines who is allowed to write to this skipchain.
	GenesisDarc darc.Darc
	// BlockInterval in int64.
	BlockInterval time.Duration
	// Maximum block size. Zero (or not present in protobuf) means use the default, 4 megs.
	// optional
	MaxBlockSize int
	// DarcContracts is the set of contracts that can be parsed as a DARC.
	// At least one contract must be given.
	DarcContractIDs []string
}

// CreateGenesisBlockResponse holds the genesis-block of the new skipchain.
type CreateGenesisBlockResponse struct {
	// Version of the protocol
	Version Version
	// Skipblock of the created skipchain or empty if there was an error.
	Skipblock *skipchain.SkipBlock
}

// AddTxRequest requests to apply a new transaction to the ledger.
type AddTxRequest struct {
	// Version of the protocol
	Version Version
	// SkipchainID is the hash of the first skipblock
	SkipchainID skipchain.SkipBlockID
	// Transaction to be applied to the kv-store
	Transaction ClientTransaction
	// How many block-intervals to wait for inclusion -
	// missing value or 0 means return immediately.
	InclusionWait int `protobuf:"opt"`
}

// AddTxResponse is the reply after an AddTxRequest is finished.
type AddTxResponse struct {
	// Version of the protocol
	Version Version
}

// GetProof returns the proof that the given key is in the trie.
type GetProof struct {
	// Version of the protocol
	Version Version
	// Key is the key we want to look up
	Key []byte
	// ID is any block that is known to us in the skipchain, can be the genesis
	// block or any later block. The proof returned will be starting at this block.
	ID skipchain.SkipBlockID
}

// GetProofResponse can be used together with the Genesis block to proof that
// the returned key/value pair is in the trie.
type GetProofResponse struct {
	// Version of the protocol
	Version Version
	// Proof contains everything necessary to prove the inclusion
	// of the included key/value pair given a genesis skipblock.
	Proof Proof
}

// CheckAuthorization returns the list of actions that could be executed if the
// signatures of the given identities are present and valid
type CheckAuthorization struct {
	// Version of the protocol
	Version Version
	// ByzCoinID where to look up the darc
	ByzCoinID skipchain.SkipBlockID
	// DarcID that holds the rules
	DarcID darc.ID
	// Identities that will sign together
	Identities []darc.Identity
}

// CheckAuthorizationResponse returns a list of Actions that the given identities
// can execute in the given darc. The list can be empty, which means that the
// given identities have now authorization in that darc at all.
type CheckAuthorizationResponse struct {
	Actions []darc.Action
}

// ChainConfig stores all the configuration information for one skipchain. It
// will be stored under the key [32]byte{} in the tree.
type ChainConfig struct {
	BlockInterval   time.Duration
	Roster          onet.Roster
	MaxBlockSize    int
	DarcContractIDs []string
}

// Proof represents everything necessary to verify a given
// key/value pair is stored in a skipchain. The proof is in three parts:
//   1. InclusionProof proves the presence or absence of the key. In case of
//   the key being present, the value is included in the proof.
//   2. Latest is used to verify the Merkle tree root used in the proof is
//   stored in the latest skipblock.
//   3. Links proves that the latest skipblock is part of the skipchain.
//
// This Structure could later be moved to cothority/skipchain.
type Proof struct {
	// InclusionProof is the deserialized InclusionProof
	InclusionProof trie.Proof
	// Providing the latest skipblock to retrieve the Merkle tree root.
	Latest skipchain.SkipBlock
	// Proving the path to the latest skipblock. The first ForwardLink has an
	// empty-sliced `From` and the genesis-block in `To`, together with the
	// roster of the genesis-block in the `NewRoster`.
	Links []skipchain.ForwardLink
}

// Instruction holds only one of Spawn, Invoke, or Delete
type Instruction struct {
	// InstanceID is either the instance that can spawn a new instance, or the instance
	// that will be invoked or deleted.
	InstanceID InstanceID
	// Spawn creates a new instance.
	Spawn *Spawn
	// Invoke calls a method of an existing instance.
	Invoke *Invoke
	// Delete removes the given instance.
	Delete *Delete
	// SignerCounter must be set to a value that is one greater than what
	// was in the last instruction signed by the same signer. Every counter
	// must map to the corresponding element in Signature. The initial
	// counter is 1. Overflow is allowed.
	SignerCounter []uint64
	// SignerIdentities are the identities of all the signers.
	SignerIdentities []darc.Identity
	// Signatures that are verified using the Darc controlling access to
	// the instance.
	Signatures [][]byte
}

// Spawn is called upon an existing instance that will spawn a new instance.
type Spawn struct {
	// ContractID represents the kind of contract that is being spawned.
	ContractID string
	// Args holds all data necessary to spawn the new instance.
	Args Arguments
}

// Invoke calls a method of an existing instance which will update its internal
// state.
type Invoke struct {
	// ContractID represents the kind of contract that is being invoked.
	ContractID string
	// Command is interpreted by the contract.
	Command string
	// Args holds all data necessary for the successful execution of the command.
	Args Arguments
}

// Delete removes the instance. The contract might enforce conditions that
// must be true before a Delete is executed.
type Delete struct {
	// ContractID represents the kind of contract that is being deleted.
	ContractID string
}

// Argument is a name/value pair that will be passed to the contract.
type Argument struct {
	// Name can be any name recognized by the contract.
	Name string
	// Value must be binary marshalled
	Value []byte
}

// ClientTransaction is a slice of Instructions that will be applied in order.
// If any of the instructions fails, none of them will be applied.
// InstructionsHash must be the hash of the concatenation of all the
// instruction hashes (see the Hash method in Instruction), this hash is what
// every instruction must sign for the transaction to be valid.
type ClientTransaction struct {
	Instructions Instructions
}

// TxResult holds a transaction and the result of running it.
type TxResult struct {
	ClientTransaction ClientTransaction
	Accepted          bool
}

// StateChange is one new state that will be applied to the collection.
type StateChange struct {
	// StateAction can be any of Create, Update, Remove
	StateAction StateAction
	// InstanceID of the state to change
	InstanceID []byte
	// ContractID points to the contract that can interpret the value
	ContractID string
	// Value is the data needed by the contract
	Value []byte
	// DarcID is the Darc controlling access to this key.
	DarcID darc.ID
	// Version is the monotonically increasing version of the instance
	Version uint64
}

// Coin is a generic structure holding any type of coin. Coins are defined
// by a genesis coin instance that is unique for each type of coin.
type Coin struct {
	// Name points to the genesis instance of that coin.
	Name InstanceID
	// Value is the total number of coins of that type.
	Value uint64
}

// StreamingRequest is a request asking the service to start streaming blocks
// on the chain specified by ID.
type StreamingRequest struct {
	ID skipchain.SkipBlockID
}

// StreamingResponse is the reply (block) that is streamed back to the client
type StreamingResponse struct {
	Block *skipchain.SkipBlock
}

// DownloadState requests the current global state of that node.
// If it is the first call to the service, then Reset
// must be true, else an error will be returned, or old data
// might be used.
type DownloadState struct {
	// ByzCoinID of the state to download
	ByzCoinID skipchain.SkipBlockID
	// Nonce is 0 for a new download, else it must be
	// equal to the nonce returned in DownloadStateResponse.
	// In case Nonce is non-zero, but doesn't correspond
	// to the current session, an error is returned,
	// as only one download-session can be active at
	// any given moment.
	Nonce uint64
	// Length of the statechanges to download
	Length int
}

// DownloadStateResponse is returned by the service. If there are no
// Instances left, then the length of Instances is 0.
type DownloadStateResponse struct {
	// KeyValues holds a copy of a slice of DBKeyValues
	// directly from bboltdb
	KeyValues []DBKeyValue
	// Nonce to be used for the download. The Nonce
	// is generated by the server, and will be set
	// for every subsequent reply, too.
	Nonce uint64
}

// DBKeyValue represents one element in bboltdb
type DBKeyValue struct {
	Key   []byte
	Value []byte
}

// StateChangeBody represents the body part of a state change, which is the
// part that needs to be serialised and stored in a merkle tree.
type StateChangeBody struct {
	StateAction StateAction
	ContractID  string
	Value       []byte
	Version     uint64
	DarcID      darc.ID
}

// GetSignerCounters is a request to get the latest version for the specified
// identity.
type GetSignerCounters struct {
	SignerIDs   []string
	SkipchainID skipchain.SkipBlockID
}

// GetSignerCountersResponse holds the latest version for the identity in the
// request.
type GetSignerCountersResponse struct {
	Counters []uint64
}

// GetInstanceVersion is a request asking the service to fetch
// the version of the given instance
type GetInstanceVersion struct {
	SkipChainID skipchain.SkipBlockID
	InstanceID  InstanceID
	Version     uint64
}

// GetLastInstanceVersion is request asking for the last version
// of a given instance
type GetLastInstanceVersion struct {
	SkipChainID skipchain.SkipBlockID
	InstanceID  InstanceID
}

// GetInstanceVersionResponse is the response for both
// GetInstanceVersion and GetLastInstanceVersion. It contains
// the state change if it exists and the block index where
// it has been applied
type GetInstanceVersionResponse struct {
	StateChange StateChange
	BlockIndex  int
}

// GetAllInstanceVersion is a request asking for the list of
// state changes of a given instance
type GetAllInstanceVersion struct {
	SkipChainID skipchain.SkipBlockID
	InstanceID  InstanceID
}

// GetAllInstanceVersionResponse is the response that contains
// the list of state changes of a instance
type GetAllInstanceVersionResponse struct {
	StateChanges []GetInstanceVersionResponse
}

// CheckStateChangeValidity is a request to get the list
// of state changes belonging to the same block as the
// targeted one to compute the hash
type CheckStateChangeValidity struct {
	SkipChainID skipchain.SkipBlockID
	InstanceID  InstanceID
	Version     uint64
}

// CheckStateChangeValidityResponse is the response with
// the list of state changes so that the hash can be
// compared against the one in the block
type CheckStateChangeValidityResponse struct {
	StateChanges []StateChange
	BlockID      skipchain.SkipBlockID
}

// DebugRequest returns the list of all byzcoins if byzcoinid is empty, else it returns
// a dump of all instances if byzcoinid is given and exists.
type DebugRequest struct {
	ByzCoinID []byte `protobuf:"opt"`
}

// DebugResponse is returned from the server. Either Byzcoins is returned and holds a
// list of all byzcoin-instances, together with the genesis block and the latest block,
// or it returns a dump of all instances in the form of a slice of StateChangeBodies.
type DebugResponse struct {
	Byzcoins []DebugResponseByzcoin `protobuf:"opt"`
	Dump     []DebugResponseState   `protobuf:"opt"`
}

// DebugResponseByzcoin represents one byzcoinid with the genesis and the latest block,
// as it is for debugging reasons, we trust the node and don't return any proof.
type DebugResponseByzcoin struct {
	ByzCoinID []byte
	Genesis   *skipchain.SkipBlock
	Latest    *skipchain.SkipBlock
}

// DebugResponseState holds one key/state pair of the response.
type DebugResponseState struct {
	Key   []byte
	State StateChangeBody
}

// DebugRemoveRequest asks the conode to delete the given byzcoin-instance from its database.
// It needs to be signed by the private key of the conode.
type DebugRemoveRequest struct {
	ByzCoinID []byte
	Signature []byte
}
