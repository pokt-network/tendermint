package health

type DataSizeMetrics struct {
	BlockSize int64
	StateSize int64
}

func (hm *HealthMetrics) AddBlockSizeMetric(height int64, blockSizeInBytes int64) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.DataSizeMetrics.BlockSize = blockSizeInBytes
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddStateSizeMetric(height int64, stateSizeInBytes int64) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height + 1)
	bm := hm.BlockMetrics[height+1]
	bm.DataSizeMetrics.StateSize = stateSizeInBytes
	hm.BlockMetrics[height+1] = bm
}
