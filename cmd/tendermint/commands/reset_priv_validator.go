package commands

import (
	"github.com/spf13/cobra"
)

// ResetAllCmd removes the database of this Tendermint core
// instance.
var ResetAllCmd = &cobra.Command{
	Use:   "unsafe_reset_all",
	Short: "(unsafe) Remove all the data and WAL, reset this node's validator to genesis state",
	Run:   resetAll,
}

var keepAddrBook bool

func init() {
	ResetAllCmd.Flags().BoolVar(&keepAddrBook, "keep-addr-book", false, "Keep the address book intact")
}

// ResetPrivValidatorCmd resets the private validator files.
var ResetPrivValidatorCmd = &cobra.Command{
	Use:   "unsafe_reset_priv_validator",
	Short: "(unsafe) Reset this node's validator to genesis state",
	Run:   resetPrivValidator,
}

// XXX: this is totally unsafe.
// it's only suitable for testnets.
func resetAll(cmd *cobra.Command, args []string) {
	panic("deprecated")
}

// XXX: this is totally unsafe.
// it's only suitable for testnets.
func resetPrivValidator(cmd *cobra.Command, args []string) {
	panic("deprecated")
}
