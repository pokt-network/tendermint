package health

type Transaction struct {
	TypeOf         string
	ProcessingTime string
	IsValid        bool
}

type TransactionMetrics struct {
	TotalTransactions int64
	TotalValidTxs     int64
	TotalInvalidTxs   int64
	Transactions      []Transaction
}

func (hm *HealthMetrics) AddTransaction(blockHeight int64, transaction Transaction) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(blockHeight+1)
	if !hm.isCheckTx {
		bm := hm.BlockMetrics[blockHeight+1]
		bm.TransactionMetrics.TotalTransactions++
		if transaction.IsValid {
			bm.TransactionMetrics.TotalValidTxs++
		} else {
			bm.TransactionMetrics.TotalInvalidTxs++
		}
		bm.TransactionMetrics.Transactions = append(bm.TransactionMetrics.Transactions, transaction)
		hm.BlockMetrics[blockHeight+1] = bm
	}
}
