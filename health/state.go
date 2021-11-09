package health

import (
	sdk "github.com/pokt-network/pocket-core/types"
	"time"
)

type StateMetrics struct {
	AppHash        string
	JailMetrics    JailMetrics
	SessionMetrics SessionMetrics
}

type SessionMetrics struct {
	SessionsGenerated         int64
	AvgMilisecondSessionTimes int64
	SessionGenerationTimes    []time.Duration
	TotalRelays               int64
}

type JailMetrics struct {
	TotalJailed      int64
	JailedValidators []Validator
}

func (hm *HealthMetrics) AddJailedValidator(ctx sdk.Ctx, val Validator) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	bm := hm.BlockMetrics[ctx.BlockHeight()]
	if !bm.IsCheckTx {
		bm.StateMetrics.JailMetrics.TotalJailed++
		bm.StateMetrics.JailMetrics.JailedValidators = append(bm.StateMetrics.JailMetrics.JailedValidators, val)
		hm.BlockMetrics[ctx.BlockHeight()] = bm
	}
}

func (hm *HealthMetrics) AddRelays(ctx sdk.Ctx, relays int64) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	bm := hm.BlockMetrics[ctx.BlockHeight()]
	if !bm.IsCheckTx {
		bm.StateMetrics.SessionMetrics.TotalRelays += relays
		hm.BlockMetrics[ctx.BlockHeight()] = bm
	}
}

func (hm *HealthMetrics) AddSessionDuration(ctx sdk.Ctx, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	bm := hm.BlockMetrics[ctx.BlockHeight()]
	if !bm.IsCheckTx {
		bm.StateMetrics.SessionMetrics.SessionsGenerated++
		bm.StateMetrics.SessionMetrics.SessionGenerationTimes = append(bm.StateMetrics.SessionMetrics.SessionGenerationTimes, d)
		milisecondsSum := int64(0)
		for _, d := range bm.StateMetrics.SessionMetrics.SessionGenerationTimes {
			milisecondsSum += d.Milliseconds()
		}
		bm.StateMetrics.SessionMetrics.AvgMilisecondSessionTimes = milisecondsSum / bm.StateMetrics.SessionMetrics.SessionsGenerated
		hm.BlockMetrics[ctx.BlockHeight()] = bm
	}
}
