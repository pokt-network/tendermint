package commands

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/privval"
)

// ShowValidatorCmd adds capabilities for showing the validator info.
var ShowValidatorCmd = &cobra.Command{
	Use:   "show_validator",
	Short: "Show this node's validator info",
	RunE:  showValidator,
}

func showValidator(cmd *cobra.Command, args []string) error {
	keyFilePath := config.PrivValidatorKeyFile()
	if !tmos.FileExists(keyFilePath) {
		return fmt.Errorf("private validator file %s does not exist", keyFilePath)
	}

	pv := privval.LoadFilePVLean(keyFilePath, config.PrivValidatorStateFile())

	pubKeys, err := pv.GetPubKeys()
	if err != nil {
		return errors.Wrap(err, "can't get pubkey")
	}
	if len(pubKeys) > 1 {
		return errors.Wrapf(err, "expected exactly one public key but got %d", len(pubKeys))
	}
	pubKey := pubKeys[0]

	bz, err := cdc.MarshalJSON(pubKey)
	if err != nil {
		return errors.Wrap(err, "failed to marshal private validator pubkey")
	}

	fmt.Println(string(bz))
	return nil
}
