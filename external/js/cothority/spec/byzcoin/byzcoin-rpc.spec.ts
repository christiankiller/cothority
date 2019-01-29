import fs from 'fs';
import { Roster } from '../../src/network/proto';
import { startConodes } from '../support/conondes';
import ByzCoinRPC from '../../src/byzcoin/byzcoin-rpc';
import SignerEd25519 from '../../src/darc/SignerEd25519';
import { DarcInstance } from '../../src/byzcoin/contracts/DarcInstance';

const data = fs.readFileSync(process.cwd() + '/spec/support/public.toml');

const blockInterval = 5 * 1000 * 1000 * 1000; // 5s in nano precision

describe('ByzCoinRPC Tests', () => {
    const roster = Roster.fromTOML(data).slice(0, 4);
    const admin = SignerEd25519.fromBytes(Buffer.from("0cb119094dbf72dfd169f8ba605069ce66a0c8ba402eb22952b544022d33b90c", "hex"));

    beforeAll(async () => {
        await startConodes();
    });

    it('should create an rpc and evolve/spawn darcs', async () => {
        const darc = ByzCoinRPC.makeGenesisDarc([admin.identity], roster);
        const rpc = await ByzCoinRPC.newByzCoinRPC(roster, darc, blockInterval);

        const proof = await rpc.getProof(Buffer.alloc(32, 0));
        expect(proof).toBeDefined();

        const instance = await DarcInstance.fromByzcoin(rpc, darc.baseID);

        const evolveDarc = darc.evolve();
        const evolveProof = await instance.evolveDarcAndWait(evolveDarc, admin, 10);
        expect(evolveProof.exists(darc.baseID)).toBeTruthy();

        const newDarc = ByzCoinRPC.makeGenesisDarc([admin.identity], roster, 'another darc');
        const newInstance = await instance.spawnDarcAndWait(newDarc, admin, 10);
        expect(newInstance.darc.baseID.equals(newDarc.baseID)).toBeTruthy();
    });
});
