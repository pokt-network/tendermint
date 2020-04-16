package client

import (
	"github.com/pokt-network/tendermint/types"
	amino "github.com/tendermint/go-amino"
)

var cdc = amino.NewCodec()

func init() {
	types.RegisterEvidences(cdc)
}
