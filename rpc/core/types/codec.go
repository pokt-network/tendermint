package core_types

import (
	"github.com/pokt-network/tendermint/types"
	amino "github.com/tendermint/go-amino"
)

func RegisterAmino(cdc *amino.Codec) {
	types.RegisterEventDatas(cdc)
	types.RegisterBlockAmino(cdc)
}
