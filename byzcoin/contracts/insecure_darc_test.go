package contracts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/cothority/v3"
	"go.dedis.ch/cothority/v3/byzcoin"
	"go.dedis.ch/cothority/v3/darc"
	"go.dedis.ch/onet/v3"
)

func TestInsecureDarc(t *testing.T) {
	local := onet.NewTCPTest(cothority.Suite)
	defer local.CloseAll()

	signer := darc.NewSignerEd25519(nil, nil)
	_, roster, _ := local.GenTree(3, true)

	genesisMsg, err := byzcoin.DefaultGenesisMsg(byzcoin.CurrentVersion, roster,
		[]string{"spawn:insecure_darc"}, signer.Identity())
	require.Nil(t, err)
	genesisMsg.DarcContractIDs = append(genesisMsg.DarcContractIDs, ContractInsecureDarcID)
	gDarc := &genesisMsg.GenesisDarc
	genesisMsg.BlockInterval = time.Second
	cl, _, err := byzcoin.NewLedger(genesisMsg, false)
	require.Nil(t, err)

	// spawn new darc
	newDarc := gDarc.Copy()
	newDarc.Description = []byte("not genesis darc")
	newDarc.Rules.AddRule("invoke:insecure_darc.evolve", []byte(signer.Identity().String()))
	newDarcBuf, err := newDarc.ToProto()
	require.NoError(t, err)
	ctx := byzcoin.ClientTransaction{
		Instructions: []byzcoin.Instruction{{
			InstanceID: byzcoin.NewInstanceID(gDarc.GetBaseID()),
			Spawn: &byzcoin.Spawn{
				ContractID: ContractInsecureDarcID,
				Args: []byzcoin.Argument{{
					Name:  "darc",
					Value: newDarcBuf,
				}},
			},
			SignerCounter: []uint64{1},
		}},
	}
	require.Nil(t, ctx.SignWith(signer))
	_, err = cl.AddTransactionAndWait(ctx, 10)
	require.NoError(t, err)

	// evolve it
	newDarc2 := newDarc.Copy()
	require.NoError(t, newDarc2.EvolveFrom(newDarc))
	newDarc2.Rules.AddRule("spawn:coin", []byte(signer.Identity().String()))
	newDarc2Buf, err := newDarc2.ToProto()
	ctx = byzcoin.ClientTransaction{
		Instructions: []byzcoin.Instruction{{
			InstanceID: byzcoin.NewInstanceID(newDarc2.GetBaseID()),
			Invoke: &byzcoin.Invoke{
				ContractID: ContractInsecureDarcID,
				Command:    "evolve",
				Args: []byzcoin.Argument{{
					Name:  "darc",
					Value: newDarc2Buf,
				}},
			},
			SignerCounter: []uint64{2},
		}},
	}
	require.Nil(t, ctx.SignWith(signer))
	_, err = cl.AddTransactionAndWait(ctx, 10)
	require.NoError(t, err)
}
