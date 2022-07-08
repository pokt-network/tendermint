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

// FilePVLean implements PrivValidator using data persisted to disk
// to prevent double signing.
// NOTE: the directories containing pv.Keys.filePath and pv.LastSignStates.filePath must already exist.
// It includes the LastSignature and LastSignBytes so we don't lose the signature
// if the process crashes after signing but before the resulting consensus message is processed.
type FilePVLean struct {
	Keys           []FilePVKey
	LastSignStates []FilePVLastSignState
	KeyFilepath    string
	StateFilepath  string
}

func GenFilePVsLean(keyFilePath, stateFilePath string, numOfKeys uint) *FilePVLean {
	filePvKeys := []FilePVKey{}
	lastSignStates := []FilePVLastSignState{}
	for i := 0; i < int(numOfKeys); i++ {
		privKey := ed25519.GenPrivKey()
		filePvKeys = append(filePvKeys, FilePVKey{
			Address:  privKey.PubKey().Address(),
			PubKey:   privKey.PubKey(),
			PrivKey:  privKey,
			filePath: keyFilePath,
		})
		lastSignStates = append(lastSignStates, FilePVLastSignState{
			Step:     stepNone,
			filePath: stateFilePath,
		})
	}
	return &FilePVLean{
		Keys:           filePvKeys,
		LastSignStates: lastSignStates,
		KeyFilepath:    keyFilePath,
		StateFilepath:  stateFilePath,
	}
}

// GenFilePVLean generates a new validator with randomly generated private key
// and sets the filePaths, but does not call Save().
func GenFilePVLean(keyFilePath, stateFilePath string) *FilePVLean {
	return GenFilePVsLean(keyFilePath, stateFilePath, 1)
}

// LoadFilePVLean loads a FilePV from the filePaths.  The FilePV handles double
// signing prevention by persisting data to the stateFilePath.  If either file path
// does not exist, the program will exit.
func LoadFilePVLean(keyFilePath, stateFilePath string) *FilePVLean {
	return loadFilePVLean(keyFilePath, stateFilePath, true)
}

// If loadState is true, we load from the stateFilePath. Otherwise, we use an empty LastSignState.
func loadFilePVLean(keyFilePath, stateFilePath string, loadState bool) *FilePVLean {
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

	return &FilePVLean{
		Keys:           pvKeys,
		LastSignStates: pvState,
		KeyFilepath:    keyFilePath,
		StateFilepath:  stateFilePath,
	}
}

// LoadOrGenFilePVLean loads a FilePV from the given filePaths
// or else generates a new one and saves it to the filePaths.
func LoadOrGenFilePVLean(keyFilePath, stateFilePath string) *FilePVLean {
	var pv *FilePVLean
	if tmos.FileExists(keyFilePath) && tmos.FileExists(stateFilePath) {
		pv = LoadFilePVLean(keyFilePath, stateFilePath)
	} else {
		panic("no key file found at " + keyFilePath + " or no state path at " + stateFilePath + " run pocket accounts set-validator(s)")
	}
	return pv
}

// SignVote signs a canonical representation of the vote, along with the
// chainID. Implements PrivValidator.
func (pv *FilePVLean) SignVote(chainID string, vote *types.Vote, key crypto.PubKey) error {
	if err := pv.signVote(chainID, vote, key); err != nil {
		return fmt.Errorf("error signing vote: %v", err)
	}
	return nil
}

// SignProposal signs a canonical representation of the proposal, along with
// the chainID. Implements PrivValidator.
func (pv *FilePVLean) SignProposal(chainID string, proposal *types.Proposal, key crypto.PubKey) error {
	if err := pv.signProposal(chainID, proposal, key); err != nil {
		return fmt.Errorf("error signing proposal: %v", err)
	}
	return nil
}

// String returns a string representation of the FilePV.
func (pv *FilePVLean) String() string {
	if len(pv.Keys) == 0 {
		return "PrivValidator empty"
	}
	return fmt.Sprintf(
		"PrivValidator{%v LH:%v, LR:%v, LS:%v}",
		pv.Keys[0].Address, // TODO make string multi validator?
		pv.LastSignStates[0].Height,
		pv.LastSignStates[0].Round,
		pv.LastSignStates[0].Step,
	)
}

func (pv *FilePVLean) GetPubKeys() ([]crypto.PubKey, error) {
	keys := make([]crypto.PubKey, len(pv.Keys))
	for i, k := range pv.Keys {
		keys[i] = k.PubKey
	}
	return keys, nil
}

//------------------------------------------------------------------------------------

func (pv *FilePVLean) GetPublicKeyIndexFromList(pubKey crypto.PubKey) (int, error) {
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
func (pv *FilePVLean) signVote(chainID string, vote *types.Vote, pubKey crypto.PubKey) error {
	height, round, step := vote.Height, vote.Round, voteToStep(vote)

	lss := pv.LastSignStates

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
	sig, err := pv.Keys[index].PrivKey.Sign(signBytes)
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
func (pv *FilePVLean) signProposal(chainID string, proposal *types.Proposal, pubKey crypto.PubKey) error {
	height, round, step := proposal.Height, proposal.Round, stepPropose

	lss := pv.LastSignStates

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
	sig, err := pv.Keys[index].PrivKey.Sign(signBytes)
	if err != nil {
		return err
	}
	pv.saveSigned(height, round, step, signBytes, sig, index)
	proposal.Signature = sig
	return nil
}

// Persist height/round/step and signature
func (pv *FilePVLean) saveSigned(height int64, round int, step int8,
	signBytes []byte, sig []byte, index int) {

	pv.LastSignStates[index].Height = height
	pv.LastSignStates[index].Round = round
	pv.LastSignStates[index].Step = step
	pv.LastSignStates[index].Signature = sig
	pv.LastSignStates[index].SignBytes = signBytes

	// backwards compatibility if you're using normal pocket
	if len(pv.LastSignStates) == 1 {
		pv.LastSignStates[index].Save()
		return
	}
	pv.SaveLastSignState()
}

func (pv *FilePVLean) SaveLastSignState() {
	outFile := pv.StateFilepath
	if outFile == "" {
		panic("cannot save FilePVLastSignState: filePath not set")
	}
	jsonBytes, err := cdc.MarshalJSONIndent(pv.LastSignStates, "", "  ")
	if err != nil {
		panic(err)
	}
	err = tempfile.WriteFileAtomic(outFile, jsonBytes, 0600)
	if err != nil {
		panic(err)
	}
}
