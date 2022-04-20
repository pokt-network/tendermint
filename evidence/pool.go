package evidence

import (
	"fmt"
	"math"
	"sync"
	"time"

	dbm "github.com/tendermint/tm-db"

	clist "github.com/tendermint/tendermint/libs/clist"
	"github.com/tendermint/tendermint/libs/log"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

// Pool maintains a pool of valid evidence
// in an Store.
type Pool struct {
	logger log.Logger

	store        *Store
	evidenceList *clist.CList // concurrent linked-list of evidence

	// needed to load validators to verify evidence
	stateDB dbm.DB

	// latest state
	mtx   sync.Mutex
	state sm.State
}

func NewPool(stateDB, evidenceDB dbm.DB) *Pool {
	store := NewStore(evidenceDB)
	evpool := &Pool{
		stateDB:      stateDB,
		state:        sm.LoadState(stateDB),
		logger:       log.NewNopLogger(),
		store:        store,
		evidenceList: clist.New(),
	}
	return evpool
}

func (evpool *Pool) EvidenceFront() *clist.CElement {
	return evpool.evidenceList.Front()
}

func (evpool *Pool) EvidenceWaitChan() <-chan struct{} {
	return evpool.evidenceList.WaitChan()
}

// SetLogger sets the Logger.
func (evpool *Pool) SetLogger(l log.Logger) {
	evpool.logger = l
}

// PriorityEvidence returns the priority evidence.
func (evpool *Pool) PriorityEvidence() []types.Evidence {
	return evpool.store.PriorityEvidence()
}

// PendingEvidence returns up to maxNum uncommitted evidence.
// If maxNum is -1, all evidence is returned.
func (evpool *Pool) PendingEvidence(maxNum int64) []types.Evidence {
	evList := evpool.store.PendingEvidence(maxNum)
	// sanity check for already committed evidence
	checkedList := make([]types.Evidence, 0)
	for _, ev := range evList {
		if evpool.IsCommitted(ev) {
			// remove the evidence so this doesn't happen again
			evpool.MarkEvidenceAsCommitted(math.MaxInt64, time.Time{}, []types.Evidence{ev})
			continue
		}
		checkedList = append(checkedList, ev)
	}
	return checkedList
}

// State returns the current state of the evpool.
func (evpool *Pool) State() sm.State {
	evpool.mtx.Lock()
	defer evpool.mtx.Unlock()
	return evpool.state
}

// Update loads the latest
func (evpool *Pool) Update(block *types.Block, state sm.State) {

	// sanity check
	if state.LastBlockHeight != block.Height {
		panic(
			fmt.Sprintf("Failed EvidencePool.Update sanity check: got state.Height=%d with block.Height=%d",
				state.LastBlockHeight,
				block.Height,
			),
		)
	}

	// update the state
	evpool.mtx.Lock()
	evpool.state = state
	evpool.mtx.Unlock()

	// remove evidence from pending and mark committed
	evpool.MarkEvidenceAsCommitted(block.Height, block.Time, block.Evidence.Evidence)
}

// IsPending checks whether the evidence is already pending. DB errors are passed to the logger.
func (evpool *Pool) IsPending(evidence types.Evidence) bool {
	ok, _ := evpool.store.HasPendingEvidence(evidence)
	return ok
}

// AddEvidence checks the evidence is valid and adds it to the pool.
func (evpool *Pool) AddEvidence(evidence types.Evidence) error {
	eHeight := evidence.Height()
	if eHeight <= 57000 {
		evpool.logger.Debug("Ignoring evidence due to patch", "ev", evidence)
		return nil
	}

	// check if evidence is already stored
	//if evpool.store.Has(evidence) {
	//	return ErrEvidenceAlreadyStored{}
	//}

	// We have already verified this piece of evidence - no need to do it again
	if evpool.IsPending(evidence) {
		evpool.logger.Info("Evidence already pending, ignoring this one", "ev", evidence)
		return nil
	}

	// check that the evidence isn't already committed
	if evpool.IsCommitted(evidence) {
		evpool.logger.Debug("Evidence was already committed, ignoring this one", "ev", evidence)
		return nil
	}

	if err := sm.VerifyEvidence(evpool.stateDB, evpool.State(), evidence); err != nil {
		return ErrInvalidEvidence{err}
	}

	// fetch the validator and return its voting power as its priority
	// TODO: something better ?
	valset, err := sm.LoadValidators(evpool.stateDB, evidence.Height())
	if err != nil {
		return err
	}
	_, val := valset.GetByAddress(evidence.Address())
	priority := val.VotingPower

	added, err := evpool.store.AddNewEvidence(evidence, priority)
	if err != nil || !added {
		return err
	}

	evpool.logger.Info("Verified new evidence of byzantine behaviour", "evidence", evidence)

	// add evidence to clist
	evpool.evidenceList.PushBack(evidence)

	return nil
}

// MarkEvidenceAsCommitted marks all the evidence as committed and removes it from the queue.
func (evpool *Pool) MarkEvidenceAsCommitted(height int64, lastBlockTime time.Time, evidence []types.Evidence) {
	// make a map of committed evidence to remove from the clist
	blockEvidenceMap := make(map[string]struct{})
	for _, ev := range evidence {
		evpool.store.MarkEvidenceAsCommitted(ev)
		blockEvidenceMap[evMapKey(ev)] = struct{}{}
	}

	// remove committed evidence from the clist
	maxAge := evpool.State().ConsensusParams.Evidence.MaxAge
	evpool.removeEvidence(height, maxAge, blockEvidenceMap)

}

// IsCommitted returns true if we have already seen this exact evidence and it is already marked as committed.
func (evpool *Pool) IsCommitted(evidence types.Evidence) bool {
	ei := evpool.store.getInfo(evidence)
	return ei.Evidence != nil && ei.Committed
}

func (evpool *Pool) removeEvidence(height, maxAge int64, blockEvidenceMap map[string]struct{}) {
	for e := evpool.evidenceList.Front(); e != nil; e = e.Next() {
		ev := e.Value.(types.Evidence)

		// Remove the evidence if it's already in a block or if it's now too old.
		if _, ok := blockEvidenceMap[evMapKey(ev)]; ok ||
			ev.Height() < height-maxAge {

			// remove from clist
			evpool.evidenceList.Remove(e)
			e.DetachPrev()
		}
	}
}

func (evpool *Pool) RollbackEvidence(height int64, latestHeight int64) {
	evpool.store.DeleteEvidenceFromHeight(height, latestHeight)
}

func evMapKey(ev types.Evidence) string {
	return string(ev.Hash())
}
