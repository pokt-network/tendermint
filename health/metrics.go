package health

import (
	sdk "github.com/pokt-network/pocket-core/types"
	"sync"
	"time"
)

func NewHealthMetrics(pruneAfter int64) *HealthMetrics {
	return &HealthMetrics{
		mtx:          sync.Mutex{},
		BlockMetrics: make(map[int64]BlockMetrics),
		PruneAfter:   pruneAfter,
	}
}

type HealthMetrics struct {
	mtx sync.Mutex
	BlockMetrics map[int64]BlockMetrics
	PruneAfter   int64
}

type BlockMetrics struct {
	Height             int64
	IsCheckTx          bool
	ConsensusMetrics   ConsensusMetrics
	DataSizeMetrics    DataSizeMetrics
	LifecycleMetrics   LifecycleMetrics
	StateMetrics       StateMetrics
	TransactionMetrics TransactionMetrics
}

func (hm *HealthMetrics) InitHeight(height int64) {
	if _, found := hm.BlockMetrics[height]; found {
		return
	}
	hm.BlockMetrics[height] = BlockMetrics{
		Height: height,
		IsCheckTx: true,
		StateMetrics: StateMetrics{
			JailMetrics: JailMetrics{
				JailedValidators: make([]Validator, 0),
			},
			SessionMetrics: SessionMetrics{
				SessionGenerationTimes: make([]time.Duration, 0),
			},
		},
		TransactionMetrics: TransactionMetrics{
			Transactions: make([]Transaction, 0),
		},
	}
}

func (hm *HealthMetrics) SetIsCheckTx(height int64, b bool) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.IsCheckTx = b
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) Prune(latestHeight int64) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	l := int64(len(hm.BlockMetrics) - 1)
	if l >= hm.PruneAfter {
		delete(hm.BlockMetrics, latestHeight-hm.PruneAfter)
	}
}

func (hm *HealthMetrics) AddServiceUrls(ctx sdk.Ctx, s ValServiceURL) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	for _, r := range hm.BlockMetrics[ctx.BlockHeight()].ConsensusMetrics.Rounds {
		for _, v := range r.PreVotes.Voters {
			v.ServiceURL = s[v.Address.String()]
		}
		for _, v := range r.PreCommits.Voters {
			v.ServiceURL = s[v.Address.String()]
		}
	}
}

type ValServiceURL map[string]string

func (vsu *ValServiceURL) NewValServiceURL() ValServiceURL {
	return make(map[string]string)
}

func (vsu *ValServiceURL) AddValidator(address sdk.Address, serviceURL string) {
	(*vsu)[address.String()] = serviceURL
}
