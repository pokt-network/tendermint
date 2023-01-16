package core

import (
	"time"

	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/p2p"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	rpctypes "github.com/tendermint/tendermint/rpc/jsonrpc/types"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

// Status returns Tendermint status including node info, pubkey, latest block
// hash, app hash, block height and time.
// More: https://docs.tendermint.com/master/rpc/#/Info/status
func Status(ctx *rpctypes.Context) (*ctypes.ResultStatus, error) {
	var (
		earliestBlockHash     tmbytes.HexBytes
		earliestAppHash       tmbytes.HexBytes
		earliestBlockTimeNano int64

		earliestBlockHeight = env.BlockStore.Base()
	)

	if earliestBlockMeta := env.BlockStore.LoadBlockMeta(earliestBlockHeight); earliestBlockMeta != nil {
		earliestAppHash = earliestBlockMeta.Header.AppHash
		earliestBlockHash = earliestBlockMeta.BlockID.Hash
		earliestBlockTimeNano = earliestBlockMeta.Header.Time.UnixNano()
	}

	var (
		latestBlockHash     tmbytes.HexBytes
		latestAppHash       tmbytes.HexBytes
		latestBlockTimeNano int64

		latestHeight = env.BlockStore.Height()
	)

	if latestHeight != 0 {
		latestBlockMeta := env.BlockStore.LoadBlockMeta(latestHeight)
		if latestBlockMeta != nil {
			latestBlockHash = latestBlockMeta.BlockID.Hash
			latestAppHash = latestBlockMeta.Header.AppHash
			latestBlockTimeNano = latestBlockMeta.Header.Time.UnixNano()
		}
	}

	// Return the very last voting power, not the voting power of this validator
	// during the last block.

	vals := validatorsAtHeight(latestUncommittedHeight())
	var vInfo []ctypes.ValidatorInfo
	for _, val := range vals {
		vInfo = append(vInfo, ctypes.ValidatorInfo{
			Address:     val.Address,
			PubKey:      val.PubKey,
			VotingPower: val.VotingPower,
		})
	}

	result := &ctypes.ResultStatus{
		NodeInfo: env.P2PTransport.NodeInfo().(p2p.DefaultNodeInfo),
		SyncInfo: ctypes.SyncInfo{
			LatestBlockHash:     latestBlockHash,
			LatestAppHash:       latestAppHash,
			LatestBlockHeight:   latestHeight,
			LatestBlockTime:     time.Unix(0, latestBlockTimeNano),
			EarliestBlockHash:   earliestBlockHash,
			EarliestAppHash:     earliestAppHash,
			EarliestBlockHeight: earliestBlockHeight,
			EarliestBlockTime:   time.Unix(0, earliestBlockTimeNano),
			CatchingUp:          env.ConsensusReactor.FastSync(),
		},
		ValidatorInfo: vInfo,
	}

	return result, nil
}

func validatorsAtHeight(h int64) (v []*types.Validator) {
	v = make([]*types.Validator, 0)
	vals, err := sm.LoadValidators(env.StateDB, h)
	if err != nil {
		return nil
	}
	for _, pubKey := range env.PubKey {
		privValAddress := pubKey.Address()
		_, val := vals.GetByAddress(privValAddress)
		if val == nil {
			continue
		}
		v = append(v, val)
	}
	return
}
