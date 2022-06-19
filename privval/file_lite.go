package privval

import (
	"bytes"
	"fmt"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/libs/tempfile"
	"github.com/tendermint/tendermint/types"
	"io/ioutil"
)

type PrivateKeyFile struct {
	PrivateKey string `json:"priv_key"`
}

//-------------------------------------------------------------------------------

//-------------------------------------------------------------------------------

// FilePVLite implements PrivValidator using data persisted to disk
// to prevent double signing.
// NOTE: the directories containing pv.Key.filePath and pv.LastSignState.filePath must already exist.
// It includes the LastSignature and LastSignBytes so we don't lose the signature
// if the process crashes after signing but before the resulting consensus message is processed.
type FilePVLite struct {
	Key           []FilePVKey
	LastSignState []FilePVLastSignState
	KeyFilepath   string
	StateFilepath string
}

// GenFilePV generates a new validator with randomly generated private key
// and sets the filePaths, but does not call Save().
func GenFilePV(keyFilePath, stateFilePath string) *FilePVLite {
	privKey := ed25519.GenPrivKey()

	return &FilePVLite{
		Key: []FilePVKey{{
			Address: privKey.PubKey().Address(),
			PubKey:  privKey.PubKey(),
			PrivKey: privKey,
		}},
		LastSignState: []FilePVLastSignState{{
			Step: stepNone,
		}},
		KeyFilepath:   keyFilePath,
		StateFilepath: stateFilePath,
	}
}

// LoadFilePV loads a FilePV from the filePaths.  The FilePV handles double
// signing prevention by persisting data to the stateFilePath.  If either file path
// does not exist, the program will exit.
func LoadFilePV(keyFilePath, stateFilePath string) *FilePVLite {
	return loadFilePV(keyFilePath, stateFilePath, true)
}

// If loadState is true, we load from the stateFilePath. Otherwise, we use an empty LastSignState.
func loadFilePV(keyFilePath, stateFilePath string, loadState bool) *FilePVLite {
	keyJSONBytes, err := ioutil.ReadFile(keyFilePath)
	if err != nil {
		tmos.Exit(err.Error())
	}

	var pvKeys []FilePVKey

	err = cdc.UnmarshalJSON(keyJSONBytes, &pvKeys)
	if err != nil {
		tmos.Exit(fmt.Sprintf("Error reading PrivValidator key from %v: %v\n", keyFilePath, err))
	}

	for _, key := range pvKeys {
		// overwrite pubkey and address for convenience
		key.PubKey = key.PrivKey.PubKey()
		key.Address = key.PubKey.Address()
	}

	var pvState []FilePVLastSignState
	if loadState {
		stateJSONBytes, err := ioutil.ReadFile(stateFilePath)
		if err != nil {
			tmos.Exit(err.Error())
		}
		err = cdc.UnmarshalJSON(stateJSONBytes, &pvState)
		if err != nil {
			tmos.Exit(fmt.Sprintf("Error reading PrivValidator state from %v: %v\n", stateFilePath, err))
		}
	}

	return &FilePVLite{
		Key:           pvKeys,
		LastSignState: pvState,
		KeyFilepath:   keyFilePath,
		StateFilepath: stateFilePath,
	}
}

// LoadOrGenFilePV loads a FilePV from the given filePaths
// or else generates a new one and saves it to the filePaths.
func LoadOrGenFilePV(keyFilePath, stateFilePath string) *FilePVLite {
	var pv *FilePVLite
	if tmos.FileExists(keyFilePath) && tmos.FileExists(stateFilePath) {
		pv = LoadFilePV(keyFilePath, stateFilePath)
	} else {
		panic("no key file found at " + keyFilePath + " or no state path at " + stateFilePath + " run pocket accounts set-validator(s)")
	}
	return pv
}

// SignVote signs a canonical representation of the vote, along with the
// chainID. Implements PrivValidator.
func (pv *FilePVLite) SignVote(chainID string, vote *types.Vote, key crypto.PubKey) error {
	if err := pv.signVote(chainID, vote, key); err != nil {
		return fmt.Errorf("error signing vote: %v", err)
	}
	return nil
}

// SignProposal signs a canonical representation of the proposal, along with
// the chainID. Implements PrivValidator.
func (pv *FilePVLite) SignProposal(chainID string, proposal *types.Proposal, key crypto.PubKey) error {
	if err := pv.signProposal(chainID, proposal, key); err != nil {
		return fmt.Errorf("error signing proposal: %v", err)
	}
	return nil
}

// String returns a string representation of the FilePV.
func (pv *FilePVLite) String() string {
	if len(pv.Key) == 0 {
		return "PrivValidator empty"
	}
	return fmt.Sprintf(
		"PrivValidator{%v LH:%v, LR:%v, LS:%v}",
		pv.Key[0].Address, // TODO make string multi validator?
		pv.LastSignState[0].Height,
		pv.LastSignState[0].Round,
		pv.LastSignState[0].Step,
	)
}

func (pv *FilePVLite) GetPubKeys() ([]crypto.PubKey, error) {
	keys := make([]crypto.PubKey, len(pv.Key))
	for i, k := range pv.Key {
		keys[i] = k.PubKey
	}
	return keys, nil
}

//------------------------------------------------------------------------------------

func (pv *FilePVLite) GetPublicKeyIndexFromList(pubKey crypto.PubKey) (int, error) {
	keys, err := pv.GetPubKeys()
	if err != nil {
		return 0, err
	}
	for i, pk := range keys {
		if pk.Equals(pubKey) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unable to find public key in the filePVLite file")
}

// signVote checks if the vote is good to sign and sets the vote signature.
// It may need to set the timestamp as well if the vote is otherwise the same as
// a previously signed vote (ie. we crashed after signing but before the vote hit the WAL).
func (pv *FilePVLite) signVote(chainID string, vote *types.Vote, pubKey crypto.PubKey) error {
	height, round, step := vote.Height, vote.Round, voteToStep(vote)

	lss := pv.LastSignState

	index, err := pv.GetPublicKeyIndexFromList(pubKey)
	if err != nil {
		return err
	}

	sameHRS, err := lss[index].CheckHRS(height, round, step)
	if err != nil {
		return err
	}

	signBytes := vote.SignBytes(chainID)

	// We might crash before writing to the wal,
	// causing us to try to re-sign for the same HRS.
	// If signbytes are the same, use the last signature.
	// If they only differ by timestamp, use last timestamp and signature
	// Otherwise, return error
	if sameHRS {
		if bytes.Equal(signBytes, lss[index].SignBytes) {
			vote.Signature = lss[index].Signature
		} else if timestamp, ok := checkVotesOnlyDifferByTimestamp(lss[index].SignBytes, signBytes); ok {
			vote.Timestamp = timestamp
			vote.Signature = lss[index].Signature
		} else {
			err = fmt.Errorf("conflicting data")
		}
		return err
	}

	// It passed the checks. Sign the vote
	sig, err := pv.Key[index].PrivKey.Sign(signBytes)
	if err != nil {
		return err
	}
	pv.saveSigned(height, round, step, signBytes, sig, index)
	vote.Signature = sig
	return nil
}

// signProposal checks if the proposal is good to sign and sets the proposal signature.
// It may need to set the timestamp as well if the proposal is otherwise the same as
// a previously signed proposal ie. we crashed after signing but before the proposal hit the WAL).
func (pv *FilePVLite) signProposal(chainID string, proposal *types.Proposal, pubKey crypto.PubKey) error {
	height, round, step := proposal.Height, proposal.Round, stepPropose

	lss := pv.LastSignState

	index, err := pv.GetPublicKeyIndexFromList(pubKey)
	if err != nil {
		return err
	}

	sameHRS, err := lss[index].CheckHRS(height, round, step)
	if err != nil {
		return err
	}

	signBytes := proposal.SignBytes(chainID)

	// We might crash before writing to the wal,
	// causing us to try to re-sign for the same HRS.
	// If signbytes are the same, use the last signature.
	// If they only differ by timestamp, use last timestamp and signature
	// Otherwise, return error
	if sameHRS {
		if bytes.Equal(signBytes, lss[index].SignBytes) {
			proposal.Signature = lss[index].Signature
		} else if timestamp, ok := checkProposalsOnlyDifferByTimestamp(lss[index].SignBytes, signBytes); ok {
			proposal.Timestamp = timestamp
			proposal.Signature = lss[index].Signature
		} else {
			err = fmt.Errorf("conflicting data")
		}
		return err
	}

	// It passed the checks. Sign the proposal
	sig, err := pv.Key[index].PrivKey.Sign(signBytes)
	if err != nil {
		return err
	}
	pv.saveSigned(height, round, step, signBytes, sig, index)
	proposal.Signature = sig
	return nil
}

// Persist height/round/step and signature
func (pv *FilePVLite) saveSigned(height int64, round int, step int8,
	signBytes []byte, sig []byte, index int) {

	pv.LastSignState[index].Height = height
	pv.LastSignState[index].Round = round
	pv.LastSignState[index].Step = step
	pv.LastSignState[index].Signature = sig
	pv.LastSignState[index].SignBytes = signBytes

	// backwards compatibility if you're using normal pocket
	if len(pv.LastSignState) == 1 {
		pv.LastSignState[index].Save()
		return
	}
	pv.SaveLastSignState()
}

func (pv *FilePVLite) SaveLastSignState() {
	outFile := pv.StateFilepath
	if outFile == "" {
		panic("cannot save FilePVLastSignState: filePath not set")
	}
	jsonBytes, err := cdc.MarshalJSONIndent(pv.LastSignState, "", "  ")
	if err != nil {
		panic(err)
	}
	err = tempfile.WriteFileAtomic(outFile, jsonBytes, 0600)
	if err != nil {
		panic(err)
	}
}
