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
	SessionsGenerated      int64
	SessionGenerationTimes []string
	TotalRelays            int64
}

type JailMetrics struct {
	TotalJailed      int64
	JailedValidators []Validator
}

func (hm *HealthMetrics) AddAppHash(ctx sdk.Ctx, commitID string) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	bm := hm.BlockMetrics[ctx.BlockHeight()]
	stateMetrics := bm.StateMetrics
	stateMetrics.AppHash = commitID
	bm.StateMetrics = stateMetrics
	hm.BlockMetrics[ctx.BlockHeight()] = bm
}

func (hm *HealthMetrics) AddJailedValidator(ctx sdk.Ctx, val Validator) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	if !hm.isCheckTx {
		bm := hm.BlockMetrics[ctx.BlockHeight()]
		stateMetrics := bm.StateMetrics
		jailMetrics := stateMetrics.JailMetrics
		jailMetrics.TotalJailed++
		jailMetrics.JailedValidators = append(jailMetrics.JailedValidators, val)
		stateMetrics.JailMetrics = jailMetrics
		bm.StateMetrics = stateMetrics
		hm.BlockMetrics[ctx.BlockHeight()] = bm
	}
}

func (hm *HealthMetrics) AddRelays(ctx sdk.Ctx, relays int64) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	if !hm.isCheckTx {
		bm := hm.BlockMetrics[ctx.BlockHeight()]
		bm.StateMetrics.SessionMetrics.TotalRelays += relays
		hm.BlockMetrics[ctx.BlockHeight()] = bm
	}
}

func (hm *HealthMetrics) AddSessionDuration(ctx sdk.Ctx, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(ctx.BlockHeight())
	if !hm.isCheckTx {
		bm := hm.BlockMetrics[ctx.BlockHeight()]
		sm := bm.StateMetrics
		sessionMetrics := sm.SessionMetrics
		sessionMetrics.SessionsGenerated += 1
		sessionMetrics.SessionGenerationTimes = append(sessionMetrics.SessionGenerationTimes, d.String())
		sm.SessionMetrics = sessionMetrics
		bm.StateMetrics = sm
		hm.BlockMetrics[ctx.BlockHeight()] = bm
	}
}
