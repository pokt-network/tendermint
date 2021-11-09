package health

import (
	"time"
)

type LifecycleMetrics struct {
	ApplyBlockTime time.Duration
	BeginBlock     time.Duration
	DeliverTxs     time.Duration
	EndBlock       time.Duration
}

func (hm *HealthMetrics) AddApplyBlocktime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.ApplyBlockTime = d
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddBeginBlockTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.BeginBlock = d
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddDeliverTxsTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.DeliverTxs = d
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddEndBlockTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.EndBlock = d
	hm.BlockMetrics[height] = bm
}
