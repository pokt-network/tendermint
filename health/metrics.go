package health

import (
	"encoding/hex"
	"encoding/json"
	sdk "github.com/pokt-network/pocket-core/types"
	"github.com/tendermint/tendermint/types"
	"sort"
	"sync"
	"strings"
)

func NewHealthMetrics(pruneAfter int64) *HealthMetrics {
	return &HealthMetrics{
		mtx:          sync.Mutex{},
		BlockMetrics: make(map[int64]BlockMetrics),
		PruneAfter:   pruneAfter,
	}
}

type HealthMetrics struct {
	mtx          sync.Mutex
	BlockMetrics map[int64]BlockMetrics
	PruneAfter   int64
	isCheckTx    bool
}

type BlockMetrics struct {
	Height             int64
	ConsensusMetrics   ConsensusMetrics
	DataSizeMetrics    DataSizeMetrics
	LifecycleMetrics   LifecycleMetrics
	StateMetrics       StateMetrics
	TransactionMetrics TransactionMetrics
}

type BlockMetricsJSON []BlockMetrics

func (hm *HealthMetrics) MarshalJSON() ([]byte, error) {
	bmArray := make(BlockMetricsJSON, 0)
	for _, bm := range hm.BlockMetrics {
		if bm.ConsensusMetrics.Rounds == nil && bm.LifecycleMetrics.BeginBlock == "" {
			continue
		}
		bmArray = append(bmArray, bm)
	}
	sort.Slice(bmArray, func(i, j int) bool {
		return bmArray[i].Height >= bmArray[j].Height
	})
	return json.Marshal(bmArray)
}

func (hm *HealthMetrics) UnmarshalJSON(data []byte) error {
	bmArr := BlockMetricsJSON{}
	hm.BlockMetrics = make(map[int64]BlockMetrics)
	err := json.Unmarshal(data, &bmArr)
	if err != nil {
		return err
	}
	for _, bm := range bmArr {
		hm.BlockMetrics[bm.Height] = bm
	}
	return nil
}

func (hm *HealthMetrics) InitHeight(height int64) {
	if _, found := hm.BlockMetrics[height]; found {
		return
	}
	hm.BlockMetrics[height] = BlockMetrics{
		Height: height,
		StateMetrics: StateMetrics{
			JailMetrics: JailMetrics{
				JailedValidators: make([]Validator, 0),
			},
			SessionMetrics: SessionMetrics{
				SessionGenerationTimes: make([]string, 0),
			},
		},
		TransactionMetrics: TransactionMetrics{
			Transactions: make([]Transaction, 0),
		},
	}
}

func (hm *HealthMetrics) SetIsCheckTx(b bool) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.isCheckTx = b
}

func (hm *HealthMetrics) Prune(latestHeight int64) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	l := int64(len(hm.BlockMetrics) - 1)
	if l >= hm.PruneAfter {
		for _, bm := range hm.BlockMetrics {
			if bm.Height <= latestHeight-hm.PruneAfter {
				delete(hm.BlockMetrics, bm.Height)
			}
		}
	}
}

func (hm *HealthMetrics) AddServiceUrls(ctx sdk.Ctx, s ValServiceURL) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	bm := hm.BlockMetrics[ctx.BlockHeight()]
	for i, r := range bm.ConsensusMetrics.Rounds {
		pv := r.PreVotes
		pc := r.PreCommits
		for _, v := range pv.Voters {
			v.ServiceURL = s[strings.ToLower(hex.EncodeToString(v.Address))].ServiceURL
			v.Power = s[hex.EncodeToString(v.Address)].Power
		}
		for _, v := range pc.Voters {
			v.ServiceURL = s[strings.ToLower(hex.EncodeToString(v.Address))].ServiceURL
			v.Power = s[strings.ToLower(hex.EncodeToString(v.Address))].Power
		}
		r.PreVotes = pv
		r.PreCommits = pc
		bm.ConsensusMetrics.Rounds[i] = r
	}
}

type ValServiceURL map[string]Validator

func (vsu *ValServiceURL) NewValServiceURL() ValServiceURL {
	return make(map[string]Validator)
}

func (vsu *ValServiceURL) AddValidator(address types.Address, serviceURL string, power int64) {
	(*vsu)[strings.ToLower(address.String())] = Validator{
		Address:    address,
		ServiceURL: serviceURL,
		Power:      power,
	}
}
