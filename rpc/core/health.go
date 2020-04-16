package core

import (
	ctypes "github.com/pokt-network/tendermint/rpc/core/types"
	rpctypes "github.com/pokt-network/tendermint/rpc/lib/types"
)

// Health gets node health. Returns empty result (200 OK) on success, no
// response - in case of an error.
// More: https://tendermint.com/rpc/#/Info/health
func Health(ctx *rpctypes.Context) (*ctypes.ResultHealth, error) {
	return &ctypes.ResultHealth{}, nil
}
