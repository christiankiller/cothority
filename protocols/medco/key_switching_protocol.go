package medco

import (
	"github.com/dedis/crypto/abstract"
	"github.com/dedis/cothority/lib/sda"
	"errors"
	"github.com/dedis/cothority/lib/dbg"
	"github.com/dedis/cothority/lib/network"
	"github.com/dedis/crypto/random"
	."github.com/dedis/cothority/services/medco/structs"
	"github.com/satori/go.uuid"
)

type KeySwitchedCipherMessage struct {
	Data map[uuid.UUID]CipherVector
	NewKey abstract.Point
	OriginalEphemeralKeys map[uuid.UUID][]abstract.Point
}

type KeySwitchedCipherStruct struct {
	*sda.TreeNode
	KeySwitchedCipherMessage
}


func init() {
	network.RegisterMessageType(KeySwitchedCipherMessage{})
	sda.ProtocolRegisterName("KeySwitching", NewKeySwitchingProtocol)
}

type KeySwitchingProtocol struct {
	*sda.TreeNodeInstance

	// Protocol feedback channel
	FeedbackChannel           chan map[uuid.UUID]CipherVector

	// Protocol communication channels
	PreviousNodeInPathChannel chan KeySwitchedCipherStruct

	// Protocol state data
	nextNodeInCircuit         *sda.TreeNode
	TargetOfSwitch            *map[uuid.UUID]CipherVector
	TargetPublicKey           *abstract.Point
	originalEphemKeys         map[uuid.UUID][]abstract.Point
}

func NewKeySwitchingProtocol(n *sda.TreeNodeInstance) (sda.ProtocolInstance, error) {
	keySwitchingProtocol := &KeySwitchingProtocol{
		TreeNodeInstance: n,
		FeedbackChannel: make(chan map[uuid.UUID]CipherVector),
	}

	if err := keySwitchingProtocol.RegisterChannel(&keySwitchingProtocol.PreviousNodeInPathChannel); err != nil {
		return nil, errors.New("couldn't register data reference channel: " + err.Error())
	}

	var i int
	var node *sda.TreeNode
	var nodeList = n.Tree().List()
	for i, node = range nodeList {
		if n.TreeNode().Equal(node) {
			keySwitchingProtocol.nextNodeInCircuit = nodeList[(i+1)%len(nodeList)]
			break
		}
	}

	return keySwitchingProtocol, nil
}

// Starts the protocol
func (p *KeySwitchingProtocol) Start() error {

	if p.TargetOfSwitch == nil {
		return errors.New("No ciphertext given as key switching target.")
	}

	if p.TargetPublicKey == nil {
		return errors.New("No new public key to be switched on provided.")
	}

	dbg.Lvl1(p.Entity(),"started a Key Switching Protocol")

	initialMap := make(map[uuid.UUID]CipherVector, len(*p.TargetOfSwitch))
	p.originalEphemKeys = make(map[uuid.UUID][]abstract.Point, len(*p.TargetOfSwitch))
	for k := range *p.TargetOfSwitch {
		initialCipherVector := *InitCipherVector(p.Suite(), len((*p.TargetOfSwitch)[k]))
		for i, c := range (*p.TargetOfSwitch)[k] {
			initialCipherVector[i].C = c.C
			p.originalEphemKeys[k][i] = c.K
		}
		initialMap[k] = initialCipherVector
	}

	p.sendToNext(&KeySwitchedCipherMessage{
		initialMap,
		*p.TargetPublicKey,
		p.originalEphemKeys})

	return nil
}

// Dispatch is an infinite loop to handle messages from channels
func (p *KeySwitchingProtocol) Dispatch() error {

	keySwitchingTarget := <- p.PreviousNodeInPathChannel

	randomnessContrib := p.Suite().Secret().Pick(random.Stream)

	for k := range keySwitchingTarget.Data {
		cv := keySwitchingTarget.Data[k]
		cv.SwitchForKey(p.Suite(), p.Private(), keySwitchingTarget.OriginalEphemeralKeys[k], keySwitchingTarget.NewKey, randomnessContrib)
	}

	if p.IsRoot() {
		dbg.Lvl1(p.Entity(), "completed key switching.")
		p.FeedbackChannel <- keySwitchingTarget.Data
	} else {
		dbg.Lvl1(p.Entity(), "carried on key switching.")
		p.sendToNext(&keySwitchingTarget.KeySwitchedCipherMessage)
	}

	return nil
}

// Sends the message msg to the next node in the circuit based on the next TreeNode in Tree.List() If not visited yet.
// If the message already visited the next node, doesn't send and returns false. Otherwise, return true.
func (p *KeySwitchingProtocol) sendToNext(msg interface{}) {
	err := p.SendTo(p.nextNodeInCircuit, msg)
	if err != nil {
		dbg.Lvl1("Had an error sending a message: ", err)
	}
}