package state

import (
	"context"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/state/txindex"
	"github.com/tendermint/tendermint/state/txindex/kv"
	"github.com/tendermint/tendermint/state/txindex/null"
	"github.com/tendermint/tendermint/store"
	db "github.com/tendermint/tm-db"
	"strings"
)

// DBProvider takes a DBContext and returns an instantiated DB.
type DBProvider func(*DBContext) (db.DB, error)

func BlocksAndStateFromDB(config *cfg.Config, dbProvider DBProvider) (blockStore *store.BlockStore, state State, blockStoreDB db.DB, stateDB db.DB, err error) {
	blockStoreDB, err = dbProvider(&DBContext{"blockstore", config})
	if err != nil {
		return
	}
	blockStore = store.NewBlockStore(blockStoreDB)
	stateDB, err = dbProvider(&DBContext{"state", config})
	if err != nil {
		return
	}
	state = LoadState(stateDB)
	return
}

// DBContext specifies config information for loading a new DB.
type DBContext struct {
	ID     string
	Config *cfg.Config
}

// DefaultDBProvider returns a database using the DBBackend and DBDir
// specified in the ctx.Config.
func DefaultDBProvider(ctx *DBContext) (db.DB, error) {
	dbType := db.BackendType(ctx.Config.DBBackend)
	return db.NewDB(ctx.ID, dbType, ctx.Config.DBDir()), nil
}

// splitAndTrimEmpty slices s into all subslices separated by sep and returns a
// slice of the string s with all leading and trailing Unicode code points
// contained in cutset removed. If sep is empty, SplitAndTrim splits after each
// UTF-8 sequence. First part is equivalent to strings.SplitN with a count of
// -1.  also filter out empty strings, only return non-empty strings.
func splitAndTrimEmpty(s, sep, cutset string) []string {
	if s == "" {
		return []string{}
	}

	spl := strings.Split(s, sep)
	nonEmptyStrings := make([]string, 0, len(spl))
	for i := 0; i < len(spl); i++ {
		element := strings.Trim(spl[i], cutset)
		if element != "" {
			nonEmptyStrings = append(nonEmptyStrings, element)
		}
	}
	return nonEmptyStrings
}

func RollbackTxIndexer(config *cfg.Config, height int64, context context.Context) error {
	var txIndexer txindex.TxIndexer
	switch config.TxIndex.Indexer {
	case "kv":
		store, err := DefaultDBProvider(&DBContext{"tx_index", config})
		if err != nil {
			return err
		}
		switch {
		case config.TxIndex.IndexKeys != "":
			txIndexer = kv.NewTxIndex(store, kv.IndexEvents(splitAndTrimEmpty(config.TxIndex.IndexKeys, ",", " ")))
		case config.TxIndex.IndexAllKeys:
			txIndexer = kv.NewTxIndex(store, kv.IndexAllEvents())
		default:
			txIndexer = kv.NewTxIndex(store)
		}
	default:
		txIndexer = &null.TxIndex{}
	}
	return txIndexer.DeleteFromHeight(context, height)
}

func RestoreStateFromBlock(stateDb db.DB, blockStore *store.BlockStore, rollbackHeight int64) State {
	validator, _ := LoadValidators(stateDb, rollbackHeight+1)
	validatorChanged := LoadValidatorsChanged(stateDb, rollbackHeight)
	lastvalidator, _ := LoadValidators(stateDb, rollbackHeight)
	nextvalidator, _ := LoadValidators(stateDb, rollbackHeight+2)

	consensusParams, _ := LoadConsensusParams(stateDb, rollbackHeight)
	consensusParamsChanged := LoadConsensusParamsChanged(stateDb, rollbackHeight)
	software := LoadSoftware(stateDb, rollbackHeight)

	block := blockStore.LoadBlock(rollbackHeight)
	nextBlock := blockStore.LoadBlock(rollbackHeight + 1)

	return State{
		ChainID: block.ChainID,
		Version: Version{Consensus: block.Version, Software: software},

		LastBlockID:                 nextBlock.LastBlockID, //? true
		LastBlockHeight:             block.Height,
		LastBlockTime:               block.Time,
		LastBlockTotalTx:            block.TotalTxs,
		NextValidators:              nextvalidator.Copy(),
		Validators:                  validator.Copy(),
		LastValidators:              lastvalidator.Copy(),
		LastHeightValidatorsChanged: validatorChanged,

		ConsensusParams:                  consensusParams,
		LastHeightConsensusParamsChanged: consensusParamsChanged,

		AppHash: nextBlock.AppHash.Bytes(),

		LastResultsHash: block.LastResultsHash,
	}
}
