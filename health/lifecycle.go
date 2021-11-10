package health

import (
	"time"
)

type LifecycleMetrics struct {
	ApplyBlockTime string
	BeginBlock     string
	DeliverTxs     string
	EndBlock       string
}

func (hm *HealthMetrics) AddApplyBlocktime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.ApplyBlockTime = d.String()
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddBeginBlockTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.BeginBlock = d.String()
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddDeliverTxsTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.DeliverTxs = d.String()
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddEndBlockTime(height int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.LifecycleMetrics.EndBlock = d.String()
	hm.BlockMetrics[height] = bm
}
