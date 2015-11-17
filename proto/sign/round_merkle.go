package sign
import (
	"github.com/dedis/cothority/lib/hashid"
	"sort"
	"github.com/dedis/cothority/lib/proof"
	"bytes"
	dbg "github.com/dedis/cothority/lib/debug_lvl"
	"github.com/dedis/crypto/abstract"
	"errors"
"github.com/dedis/cothority/lib/coconet"
)

const FIRST_ROUND int = 1 // start counting rounds at 1

type RoundMerkle struct {
								   // Message created by root. It can be empty and it will make no difference. In
								   // the case of a timestamp service however we need the timestamp generated by
								   // the round for this round . It will be included in the challenge, and then
								   // can be verified by the client
	Msg            []byte
	C              abstract.Secret // round lasting challenge
	R              abstract.Secret // round lasting response

	Log            SNLog           // round lasting log structure
	HashedLog      []byte

	R_hat          abstract.Secret // aggregate of responses

	X_hat          abstract.Point  // aggregate of public keys

	Commits        []*SigningMessage
	Responses      []*SigningMessage

								   // own big merkle subtree
	MTRoot         hashid.HashId   // mt root for subtree, passed upwards
	Leaves         []hashid.HashId // leaves used to build the merkle subtre
	LeavesFrom     []string        // child names for leaves

								   // mtRoot before adding HashedLog
	LocalMTRoot    hashid.HashId

								   // merkle tree roots of children in strict order
	CMTRoots       []hashid.HashId
	CMTRootNames   []string
	Proofs         map[string]proof.Proof
	Proof          []hashid.HashId
	PubKey         abstract.Point
	PrivKey        abstract.Secret
	Name           string

								   // round-lasting public keys of children servers that did not
								   // respond to latest commit or respond phase, in subtree
	ExceptionList  []abstract.Point
								   // combined point commits of children servers in subtree
	ChildV_hat     map[string]abstract.Point
								   // combined public keys of children servers in subtree
	ChildX_hat     map[string]abstract.Point
								   // for internal verification purposes
	ExceptionX_hat abstract.Point
	ExceptionV_hat abstract.Point

	BackLink       hashid.HashId
	AccRound       []byte

	Vote           *Vote
	Suite          abstract.Suite

	Children       map[string]coconet.Conn
	Parent         string
	View           int
}

type RoundType int

const (
	EmptyRT RoundType = iota
	ViewChangeRT
	AddRT
	RemoveRT
	ShutdownRT
	NoOpRT
	SigningRT
)

func NewRound(suite abstract.Suite) *RoundMerkle {
	round := &RoundMerkle{}
	round.Commits = make([]*SigningMessage, 0)
	round.Responses = make([]*SigningMessage, 0)
	round.ExceptionList = make([]abstract.Point, 0)
	round.Suite = suite
	round.Log.Suite = suite
	return round
}

// Sets up a round according to the needs stated in the
// Announcementmessage.
func RoundSetup(sn *Node, view int, am *AnnouncementMessage) error {
	sn.viewmu.Lock()
	if sn.ChangingView && am.Vote != nil && am.Vote.Vcv == nil {
		dbg.Lvl4(sn.Name(), "currently chaning view")
		sn.viewmu.Unlock()
		return ChangingViewError
	}
	sn.viewmu.Unlock()

	sn.roundmu.Lock()
	roundNbr := am.RoundNbr
	if roundNbr <= sn.LastSeenRound {
		sn.roundmu.Unlock()
		return ErrPastRound
	}

	// make space for round type
	if len(sn.RoundTypes) <= roundNbr {
		sn.RoundTypes = append(sn.RoundTypes, make([]RoundType, max(len(sn.RoundTypes), roundNbr + 1))...)
	}
	if am.Vote == nil {
		dbg.Lvl4(roundNbr, len(sn.RoundTypes))
		sn.RoundTypes[roundNbr] = SigningRT
	} else {
		sn.RoundTypes[roundNbr] = RoundType(am.Vote.Type)
	}
	sn.roundmu.Unlock()

	// set up commit and response channels for the new round
	round := NewRound(sn.suite)
	round.Vote = am.Vote
	round.Children = sn.Children(view)
	round.Parent = sn.Parent(view)
	round.View = view
	round.PubKey = sn.PubKey
	round.PrivKey = sn.PrivKey
	round.Name = sn.Name()
	round.InitCommitCrypto()
	sn.Rounds[roundNbr] = round

	// update max seen round
	sn.roundmu.Lock()
	sn.LastSeenRound = max(sn.LastSeenRound, roundNbr)
	sn.roundmu.Unlock()

	// the root is the only node that keeps track of round # internally
	if sn.IsRoot(view) {
		sn.RoundsAsRoot += 1
		// TODO: is sn.Round needed if we have LastSeenRound
		sn.Round = roundNbr

		// Create my back link to previous round
		sn.SetBackLink(roundNbr)
		// sn.SetAccountableRound(Round)
	}
	return nil
}

func (rt RoundType) String() string {
	switch rt {
	case EmptyRT:
		return "empty"
	case SigningRT:
		return "signing"
	case ViewChangeRT:
		return "viewchange"
	case AddRT:
		return "add"
	case RemoveRT:
		return "remove"
	case ShutdownRT:
		return "shutdown"
	case NoOpRT:
		return "noop"
	default:
		return ""
	}
}

/*
 * This is a module for the round-struct that does all the
 * calculation for a merkle-hash-tree.
 */

// Create round lasting secret and commit point v and V
// Initialize log structure for the round
func (round *RoundMerkle) InitCommitCrypto() {
	// generate secret and point commitment for this round
	rand := round.Suite.Cipher([]byte(round.Name))
	round.Log = SNLog{}
	round.Log.v = round.Suite.Secret().Pick(rand)
	round.Log.V = round.Suite.Point().Mul(nil, round.Log.v)
	// initialize product of point commitments
	round.Log.V_hat = round.Suite.Point().Null()
	round.Log.Suite = round.Suite
	round.Add(round.Log.V_hat, round.Log.V)

	round.X_hat = round.Suite.Point().Null()
	round.Add(round.X_hat, round.PubKey)
}

// Adds a child-node to the Merkle-tree and updates the root-hashes
func (round *RoundMerkle) MerkleAddChildren() {
	// children commit roots
	round.CMTRoots = make([]hashid.HashId, len(round.Leaves))
	copy(round.CMTRoots, round.Leaves)
	round.CMTRootNames = make([]string, len(round.Leaves))
	copy(round.CMTRootNames, round.LeavesFrom)

	// concatenate children commit roots in one binary blob for easy marshalling
	round.Log.CMTRoots = make([]byte, 0)
	for _, leaf := range round.Leaves {
		round.Log.CMTRoots = append(round.Log.CMTRoots, leaf...)
	}
}

// Adds the local Merkle-tree root, usually from a stamper or
// such
func (round *RoundMerkle) MerkleAddLocal(localMTroot hashid.HashId) {
	// add own local mtroot to leaves
	round.LocalMTRoot = localMTroot
	round.Leaves = append(round.Leaves, round.LocalMTRoot)
}

// Hashes the log of the round-structure
func (round *RoundMerkle) MerkleHashLog() error {
	var err error

	h := round.Suite.Hash()
	logBytes, err := round.Log.MarshalBinary()
	if err != nil {
		return err
	}
	h.Write(logBytes)
	round.HashedLog = h.Sum(nil)
	return err
}


func (round *RoundMerkle) ComputeCombinedMerkleRoot() {
	// add hash of whole log to leaves
	round.Leaves = append(round.Leaves, round.HashedLog)

	// compute MT root based on Log as right child and
	// MT of leaves as left child and send it up to parent
	sort.Sort(hashid.ByHashId(round.Leaves))
	left, proofs := proof.ProofTree(round.Suite.Hash, round.Leaves)
	right := round.HashedLog
	moreLeaves := make([]hashid.HashId, 0)
	moreLeaves = append(moreLeaves, left, right)
	round.MTRoot, _ = proof.ProofTree(round.Suite.Hash, moreLeaves)

	// Hashed Log has to come first in the proof; len(sn.CMTRoots)+1 proofs
	round.Proofs = make(map[string]proof.Proof, 0)
	for name := range round.Children {
		round.Proofs[name] = append(round.Proofs[name], right)
	}
	round.Proofs["local"] = append(round.Proofs["local"], right)

	// separate proofs by children (need to send personalized proofs to children)
	// also separate local proof (need to send it to timestamp server)
	round.SeparateProofs(proofs, round.Leaves)
}

// Identify which proof corresponds to which leaf
// Needed given that the leaves are sorted before passed to the function that create
// the Merkle Tree and its Proofs
func (round *RoundMerkle) SeparateProofs(proofs []proof.Proof, leaves []hashid.HashId) {
	// separate proofs for children servers mt roots
	for i := 0; i < len(round.CMTRoots); i++ {
		name := round.CMTRootNames[i]
		for j := 0; j < len(leaves); j++ {
			if bytes.Compare(round.CMTRoots[i], leaves[j]) == 0 {
				// sn.Proofs[i] = append(sn.Proofs[i], proofs[j]...)
				round.Proofs[name] = append(round.Proofs[name], proofs[j]...)
				continue
			}
		}
	}

	// separate proof for local mt root
	for j := 0; j < len(leaves); j++ {
		if bytes.Compare(round.LocalMTRoot, leaves[j]) == 0 {
			round.Proofs["local"] = append(round.Proofs["local"], proofs[j]...)
		}
	}
}

func (round *RoundMerkle) InitResponseCrypto() {
	round.R = round.Suite.Secret()
	round.R.Mul(round.PrivKey, round.C).Sub(round.Log.v, round.R)
	// initialize sum of children's responses
	round.R_hat = round.R
}

// Create Merkle Proof for local client (timestamp server) and
// store it in Node so that we can send it to the clients during
// the SignatureBroadcast
func (round *RoundMerkle) StoreLocalMerkleProof(chm *ChallengeMessage) error {
	proofForClient := make(proof.Proof, len(chm.Proof))
	copy(proofForClient, chm.Proof)

	// To the proof from our root to big root we must add the separated proof
	// from the localMKT of the client (timestamp server) to our root
	proofForClient = append(proofForClient, round.Proofs["local"]...)

	// if want to verify partial and full proofs
	if dbg.DebugVisible > 2 {
		//sn.VerifyAllProofs(view, chm, proofForClient)
	}
	round.Proof = proofForClient
	round.MTRoot = chm.MTRoot
	return nil
}

// Figure out which kids did not submit messages
// Add default messages to messgs, one per missing child
// as to make it easier to identify and add them to exception lists in one place
func (round *RoundMerkle) FillInWithDefaultMessages() []*SigningMessage {
	children := round.Children

	messgs := round.Responses
	allmessgs := make([]*SigningMessage, len(messgs))
	copy(allmessgs, messgs)

	for c := range children {
		found := false
		for _, m := range messgs {
			if m.From == c {
				found = true
				break
			}
		}

		if !found {
			allmessgs = append(allmessgs, &SigningMessage{View: round.View,
				Type: Default, From: c})
		}
	}

	return allmessgs
}

// Called by every node after receiving aggregate responses from descendants
func (round *RoundMerkle) VerifyResponses() error {

	// Check that: base**r_hat * X_hat**c == V_hat
	// Equivalent to base**(r+xc) == base**(v) == T in vanillaElGamal
	Aux := round.Suite.Point()
	V_clean := round.Suite.Point()
	V_clean.Add(V_clean.Mul(nil, round.R_hat), Aux.Mul(round.X_hat, round.C))
	// T is the recreated V_hat
	T := round.Suite.Point().Null()
	T.Add(T, V_clean)
	T.Add(T, round.ExceptionV_hat)

	var c2 abstract.Secret
	isroot := round.Parent == ""
	if isroot {
		// round challenge must be recomputed given potential
		// exception list
		msg := round.Msg
		msg = append(msg, []byte(round.MTRoot)...)
		round.C = HashElGamal(round.Suite, msg, round.Log.V_hat)
		c2 = HashElGamal(round.Suite, msg, T)
	}

	// intermediary nodes check partial responses aginst their partial keys
	// the root node is also able to check against the challenge it emitted
	if !T.Equal(round.Log.V_hat) || (isroot && !round.C.Equal(c2)) {
		return errors.New("Verifying ElGamal Collective Signature failed in " +
		round.Name)
	} else if isroot {
		dbg.Lvl4(round.Name, "reports ElGamal Collective Signature succeeded")
	}
	return nil
}

// Create Personalized Merkle Proofs for children servers
// Send Personalized Merkle Proofs to children servers
func (round *RoundMerkle) SendChildrenChallengesProofs(chm *ChallengeMessage) error {
	// proof from big root to our root will be sent to all children
	baseProof := make(proof.Proof, len(chm.Proof))
	copy(baseProof, chm.Proof)

	// for each child, create personalized part of proof
	// embed it in SigningMessage, and send it
	for name, conn := range round.Children {
		newChm := *chm
		newChm.Proof = append(baseProof, round.Proofs[name]...)

		var messg coconet.BinaryMarshaler
		messg = &SigningMessage{View: round.View, Type: Challenge, Chm: &newChm}

		// send challenge message to child
		// dbg.Lvl4("connection: sending children challenge proofs:", name, conn)
		if err := conn.PutData(messg); err != nil {
			return err
		}
	}

	return nil
}

// Send children challenges
func (round *RoundMerkle) SendChildrenChallenges(chm *ChallengeMessage) error {
	for _, child := range round.Children {
		var messg coconet.BinaryMarshaler
		messg = &SigningMessage{View: round.View, Type: Challenge, Chm: chm}

		// fmt.Println(sn.Name(), "send to", i, child, "on view", view)
		if err := child.PutData(messg); err != nil {
			return err
		}
	}

	return nil
}


// Adding-function for crypto-points that accepts nil
func (r *RoundMerkle) Add(a abstract.Point, b abstract.Point) {
	if a == nil {
		a = r.Suite.Point().Null()
	}
	if b != nil {
		a.Add(a, b)
	}
}

// Substraction-function for crypto-points that accepts nil
func (r *RoundMerkle) Sub(a abstract.Point, b abstract.Point) {
	if a == nil {
		a = r.Suite.Point().Null()
	}
	if b != nil {
		a.Sub(a, b)
	}
}

func (r *RoundMerkle) IsRoot() bool {
	return r.Parent == ""
}

func (r *RoundMerkle) IsLeaf() bool {
	return len(r.Children) == 0
}
