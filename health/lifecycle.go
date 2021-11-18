package health

import (
	"time"
)

type LifecycleMetrics struct {
	ApplyBlockTime int64
	BeginBlock     int64
	DeliverTxs     int64
	EndBlock       int64
}

func (hm *HealthMetrics) AddApplyBlocktime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.ApplyBlockTime = d.Milliseconds()
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddBeginBlockTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.BeginBlock = d.Milliseconds()
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddDeliverTxsTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.DeliverTxs = d.Milliseconds()
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddEndBlockTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.EndBlock = d.Milliseconds()
	hm.BlockMetrics[height] = bm
}
