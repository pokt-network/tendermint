package types

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/crypto/tmhash"
	"github.com/tendermint/tendermint/libs/bits"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmmath "github.com/tendermint/tendermint/libs/math"
	tmproto "github.com/tendermint/tendermint/proto/types"
	tmversion "github.com/tendermint/tendermint/proto/version"
	"github.com/tendermint/tendermint/version"
)

const (
	// MaxHeaderBytes is a maximum header size (including amino overhead).
	MaxHeaderBytes int64 = 653

	// MaxAminoOverheadForBlock - maximum amino overhead to encode a block (up to
	// MaxBlockSizeBytes in size) not including it's parts except Data.
	// This means it also excludes the overhead for individual transactions.
	// To compute individual transactions' overhead use types.ComputeAminoOverhead(tx types.Tx, fieldNum int).
	//
	// Uvarint length of MaxBlockSizeBytes: 4 bytes
	// 2 fields (2 embedded):               2 bytes
	// Uvarint length of Data.Txs:          4 bytes
	// Data.Txs field:                      1 byte
	MaxAminoOverheadForBlock int64 = 11
)

// Block defines the atomic unit of a Tendermint blockchain.
type Block struct {
	mtx sync.Mutex

	Header     `json:"header"`
	Data       `json:"data"`
	Evidence   EvidenceData `json:"evidence"`
	LastCommit *Commit      `json:"last_commit"`
}

// ValidateBasic performs basic validation that doesn't involve state data.
// It checks the internal consistency of the block.
// Further validation is done using state#ValidateBlock.
func (b *Block) ValidateBasic() error {
	if b == nil {
		return errors.New("nil block")
	}

	b.mtx.Lock()
	defer b.mtx.Unlock()

	if err := b.Header.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid header: %w", err)
	}

	// Validate the last commit and its hash.
	if b.Header.Height > 1 {
		if b.LastCommit == nil {
			return errors.New("nil LastCommit")
		}
		if err := b.LastCommit.ValidateBasic(); err != nil {
			return fmt.Errorf("wrong LastCommit: %v", err)
		}
	}
	if err := ValidateHash(b.LastCommitHash); err != nil {
		return fmt.Errorf("Wrong Header.LastCommitHash: %v", err)
	}
	if !bytes.Equal(b.LastCommitHash, b.LastCommit.Hash()) {
		return fmt.Errorf("wrong Header.LastCommitHash. Expected %v, got %v",
			b.LastCommit.Hash(),
			b.LastCommitHash,
		)
	}

	// NOTE: Timestamp validation is subtle and handled elsewhere.
	newTxs := int64(len(b.Data.Txs))
	if b.NumTxs != newTxs {
		return fmt.Errorf("Wrong Header.NumTxs. Expected %v, got %v",
			newTxs,
			b.NumTxs,
		)
	}

	// Validate the hash of the transactions.
	// NOTE: b.Data.Txs may be nil, but b.Data.Hash()
	// still works fine
	if err := ValidateHash(b.DataHash); err != nil {
		return fmt.Errorf("Wrong Header.DataHash: %v", err)
	}
	if !bytes.Equal(b.DataHash, b.Data.Hash()) {
		return fmt.Errorf(
			"wrong Header.DataHash. Expected %v, got %v",
			b.Data.Hash(),
			b.DataHash,
		)
	}

	// Basic validation of hashes related to application data.
	// Will validate fully against state in state#ValidateBlock.
	if err := ValidateHash(b.ValidatorsHash); err != nil {
		return fmt.Errorf("Wrong Header.ValidatorsHash: %v", err)
	}
	if err := ValidateHash(b.NextValidatorsHash); err != nil {
		return fmt.Errorf("Wrong Header.NextValidatorsHash: %v", err)
	}
	if err := ValidateHash(b.ConsensusHash); err != nil {
		return fmt.Errorf("Wrong Header.ConsensusHash: %v", err)
	}
	// NOTE: AppHash is arbitrary length
	if err := ValidateHash(b.LastResultsHash); err != nil {
		return fmt.Errorf("Wrong Header.LastResultsHash: %v", err)
	}

	// Validate evidence and its hash.
	if err := ValidateHash(b.EvidenceHash); err != nil {
		return fmt.Errorf("Wrong Header.EvidenceHash: %v", err)
	}
	// NOTE: b.Evidence.Evidence may be nil, but we're just looping.
	for i, ev := range b.Evidence.Evidence {
		if err := ev.ValidateBasic(); err != nil {
			return fmt.Errorf("invalid evidence (#%d): %v", i, err)
		}
	}

	if !bytes.Equal(b.EvidenceHash, b.Evidence.Hash()) {
		return fmt.Errorf("wrong Header.EvidenceHash. Expected %v, got %v",
			b.EvidenceHash,
			b.Evidence.Hash(),
		)
	}

	if len(b.ProposerAddress) != crypto.AddressSize {
		return fmt.Errorf("Expected len(Header.ProposerAddress) to be %d, got %d",
			crypto.AddressSize, len(b.ProposerAddress))
	}

	return nil
}

// fillHeader fills in any remaining header fields that are a function of the block data
func (b *Block) fillHeader() {
	if b.LastCommitHash == nil {
		b.LastCommitHash = b.LastCommit.Hash()
	}
	if b.DataHash == nil {
		b.DataHash = b.Data.Hash()
	}
	if b.EvidenceHash == nil {
		b.EvidenceHash = b.Evidence.Hash()
	}
}

// Hash computes and returns the block hash.
// If the block is incomplete, block hash is nil for safety.
func (b *Block) Hash() tmbytes.HexBytes {
	if b == nil {
		return nil
	}
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.LastCommit == nil {
		return nil
	}
	b.fillHeader()
	return b.Header.Hash()
}

// MakePartSet returns a PartSet containing parts of a serialized block.
// This is the form in which the block is gossipped to peers.
// CONTRACT: partSize is greater than zero.
func (b *Block) MakePartSet(partSize int) *PartSet {
	if b == nil {
		return nil
	}
	b.mtx.Lock()
	defer b.mtx.Unlock()

	// We prefix the byte length, so that unmarshaling
	// can easily happen via a reader.
	bz, err := cdc.MarshalBinaryLengthPrefixed(b)
	if err != nil {
		panic(err)
	}
	return NewPartSetFromData(bz, partSize)
}

// HashesTo is a convenience function that checks if a block hashes to the given argument.
// Returns false if the block is nil or the hash is empty.
func (b *Block) HashesTo(hash []byte) bool {
	if len(hash) == 0 {
		return false
	}
	if b == nil {
		return false
	}
	return bytes.Equal(b.Hash(), hash)
}

// Size returns size of the block in bytes.
func (b *Block) Size() int {
	bz, err := cdc.MarshalBinaryBare(b)
	if err != nil {
		return 0
	}
	return len(bz)
}

// String returns a string representation of the block
func (b *Block) String() string {
	return b.StringIndented("")
}

// StringIndented returns a string representation of the block
func (b *Block) StringIndented(indent string) string {
	if b == nil {
		return "nil-Block"
	}
	return fmt.Sprintf(`Block{
%s  %v
%s  %v
%s  %v
%s  %v
%s}#%v`,
		indent, b.Header.StringIndented(indent+"  "),
		indent, b.Data.StringIndented(indent+"  "),
		indent, b.Evidence.StringIndented(indent+"  "),
		indent, b.LastCommit.StringIndented(indent+"  "),
		indent, b.Hash())
}

// StringShort returns a shortened string representation of the block
func (b *Block) StringShort() string {
	if b == nil {
		return "nil-Block"
	}
	return fmt.Sprintf("Block#%v", b.Hash())
}

//-----------------------------------------------------------
// These methods are for Protobuf Compatibility

// Marshal returns the amino encoding.
func (b *Block) Marshal() ([]byte, error) {
	return cdc.MarshalBinaryBare(b)
}

// MarshalTo calls Marshal and copies to the given buffer.
func (b *Block) MarshalTo(data []byte) (int, error) {
	bs, err := b.Marshal()
	if err != nil {
		return -1, err
	}
	return copy(data, bs), nil
}

// Unmarshal deserializes from amino encoded form.
func (b *Block) Unmarshal(bs []byte) error {
	return cdc.UnmarshalBinaryBare(bs, b)
}

//-----------------------------------------------------------------------------

// MaxDataBytes returns the maximum size of block's data.
//
// XXX: Panics on negative result.
func MaxDataBytes(maxBytes int64, valsCount, evidenceCount int) int64 {
	maxDataBytes := maxBytes -
		MaxAminoOverheadForBlock -
		MaxHeaderBytes -
		int64(valsCount)*MaxVoteBytes -
		int64(evidenceCount)*MaxEvidenceBytes

	if maxDataBytes < 0 {
		panic(fmt.Sprintf(
			"Negative MaxDataBytes. Block.MaxBytes=%d is too small to accommodate header&lastCommit&evidence=%d",
			maxBytes,
			-(maxDataBytes - maxBytes),
		))
	}

	return maxDataBytes

}

// MaxDataBytesUnknownEvidence returns the maximum size of block's data when
// evidence count is unknown. MaxEvidencePerBlock will be used for the size
// of evidence.
//
// XXX: Panics on negative result.
func MaxDataBytesUnknownEvidence(maxBytes int64, valsCount int) int64 {
	_, maxEvidenceBytes := MaxEvidencePerBlock(maxBytes)
	maxDataBytes := maxBytes -
		MaxAminoOverheadForBlock -
		MaxHeaderBytes -
		int64(valsCount)*MaxVoteBytes -
		maxEvidenceBytes

	if maxDataBytes < 0 {
		panic(fmt.Sprintf(
			"Negative MaxDataBytesUnknownEvidence. Block.MaxBytes=%d is too small to accommodate header&lastCommit&evidence=%d",
			maxBytes,
			-(maxDataBytes - maxBytes),
		))
	}

	return maxDataBytes
}

//-----------------------------------------------------------------------------

// Header defines the structure of a Tendermint block header.
// NOTE: changes to the Header should be duplicated in:
// - header.Hash()
// - abci.Header
// - https://github.com/tendermint/spec/blob/master/spec/blockchain/blockchain.md
type Header struct {
	// basic block info
	Version  version.Consensus `json:"version"`
	ChainID  string            `json:"chain_id"`
	Height   int64             `json:"height"`
	Time     time.Time         `json:"time"`
	NumTxs   int64             `json:"num_txs"`
	TotalTxs int64             `json:"total_txs"`

	// prev block info
	LastBlockID BlockID `json:"last_block_id"`

	// hashes of block data
	LastCommitHash tmbytes.HexBytes `json:"last_commit_hash"` // commit from validators from the last block
	DataHash       tmbytes.HexBytes `json:"data_hash"`        // transactions

	// hashes from the app output from the prev block
	ValidatorsHash     tmbytes.HexBytes `json:"validators_hash"`      // validators for the current block
	NextValidatorsHash tmbytes.HexBytes `json:"next_validators_hash"` // validators for the next block
	ConsensusHash      tmbytes.HexBytes `json:"consensus_hash"`       // consensus params for current block
	AppHash            tmbytes.HexBytes `json:"app_hash"`             // state after txs from the previous block
	// root hash of all results from the txs from the previous block
	LastResultsHash tmbytes.HexBytes `json:"last_results_hash"`

	// consensus info
	EvidenceHash    tmbytes.HexBytes `json:"evidence_hash"`    // evidence included in the block
	ProposerAddress Address          `json:"proposer_address"` // original proposer of the block
}

// Populate the Header with state-derived data.
// Call this after MakeBlock to complete the Header.
func (h *Header) Populate(
	version version.Consensus, chainID string,
	timestamp time.Time, lastBlockID BlockID, totalTxs int64,
	valHash, nextValHash []byte,
	consensusHash, appHash, lastResultsHash []byte,
	proposerAddress Address,
) {
	h.Version = version
	h.ChainID = chainID
	h.Time = timestamp
	h.LastBlockID = lastBlockID
	h.TotalTxs = totalTxs
	h.ValidatorsHash = valHash
	h.NextValidatorsHash = nextValHash
	h.ConsensusHash = consensusHash
	h.AppHash = appHash
	h.LastResultsHash = lastResultsHash
	h.ProposerAddress = proposerAddress
}

// ValidateBasic performs stateless validation on a Header returning an error
// if any validation fails.
//
// NOTE: Timestamp validation is subtle and handled elsewhere.
func (h Header) ValidateBasic() error {
	if len(h.ChainID) > MaxChainIDLen {
		return fmt.Errorf("chainID is too long; got: %d, max: %d", len(h.ChainID), MaxChainIDLen)
	}

	if h.Height < 0 {
		return errors.New("negative Height")
	} else if h.Height == 0 {
		return errors.New("zero Height")
	}

	if err := h.LastBlockID.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong LastBlockID: %w", err)
	}

	if err := ValidateHash(h.LastCommitHash); err != nil {
		return fmt.Errorf("wrong LastCommitHash: %v", err)
	}

	if err := ValidateHash(h.DataHash); err != nil {
		return fmt.Errorf("wrong DataHash: %v", err)
	}

	if err := ValidateHash(h.EvidenceHash); err != nil {
		return fmt.Errorf("wrong EvidenceHash: %v", err)
	}

	if len(h.ProposerAddress) != crypto.AddressSize {
		return fmt.Errorf(
			"invalid ProposerAddress length; got: %d, expected: %d",
			len(h.ProposerAddress), crypto.AddressSize,
		)
	}

	// Basic validation of hashes related to application data.
	// Will validate fully against state in state#ValidateBlock.
	if err := ValidateHash(h.ValidatorsHash); err != nil {
		return fmt.Errorf("wrong ValidatorsHash: %v", err)
	}
	if err := ValidateHash(h.NextValidatorsHash); err != nil {
		return fmt.Errorf("wrong NextValidatorsHash: %v", err)
	}
	if err := ValidateHash(h.ConsensusHash); err != nil {
		return fmt.Errorf("wrong ConsensusHash: %v", err)
	}
	// NOTE: AppHash is arbitrary length
	if err := ValidateHash(h.LastResultsHash); err != nil {
		return fmt.Errorf("wrong LastResultsHash: %v", err)
	}

	return nil
}

// Hash returns the hash of the header.
// It computes a Merkle tree from the header fields
// ordered as they appear in the Header.
// Returns nil if ValidatorHash is missing,
// since a Header is not valid unless there is
// a ValidatorsHash (corresponding to the validator set).
func (h *Header) Hash() tmbytes.HexBytes {
	if h == nil || len(h.ValidatorsHash) == 0 {
		return nil
	}
	return merkle.SimpleHashFromByteSlices([][]byte{
		cdcEncode(h.Version),
		cdcEncode(h.ChainID),
		cdcEncode(h.Height),
		cdcEncode(h.Time),
		cdcEncode(h.NumTxs),
		cdcEncode(h.TotalTxs),
		cdcEncode(h.LastBlockID),
		cdcEncode(h.LastCommitHash),
		cdcEncode(h.DataHash),
		cdcEncode(h.ValidatorsHash),
		cdcEncode(h.NextValidatorsHash),
		cdcEncode(h.ConsensusHash),
		cdcEncode(h.AppHash),
		cdcEncode(h.LastResultsHash),
		cdcEncode(h.EvidenceHash),
		cdcEncode(h.ProposerAddress),
	})
}

// StringIndented returns a string representation of the header
func (h *Header) StringIndented(indent string) string {
	if h == nil {
		return "nil-Header"
	}
	return fmt.Sprintf(`Header{
%s  Version:        %v
%s  ChainID:        %v
%s  Height:         %v
%s  Time:           %v
%s  NumTxs:         %v
%s  TotalTxs:       %v
%s  LastBlockID:    %v
%s  LastCommit:     %v
%s  Data:           %v
%s  Validators:     %v
%s  NextValidators: %v
%s  App:            %v
%s  Consensus:      %v
%s  Results:        %v
%s  Evidence:       %v
%s  Proposer:       %v
%s}#%v`,
		indent, h.Version,
		indent, h.ChainID,
		indent, h.Height,
		indent, h.Time,
		indent, h.NumTxs,
		indent, h.TotalTxs,
		indent, h.LastBlockID,
		indent, h.LastCommitHash,
		indent, h.DataHash,
		indent, h.ValidatorsHash,
		indent, h.NextValidatorsHash,
		indent, h.AppHash,
		indent, h.ConsensusHash,
		indent, h.LastResultsHash,
		indent, h.EvidenceHash,
		indent, h.ProposerAddress,
		indent, h.Hash())
}

// ToProto converts Header to protobuf
func (h *Header) ToProto() *tmproto.Header {
	if h == nil {
		return nil
	}
	return &tmproto.Header{
		Version:            tmversion.Consensus{Block: h.Version.App.Uint64(), App: h.Version.App.Uint64()},
		ChainID:            h.ChainID,
		Height:             h.Height,
		Time:               h.Time,
		LastBlockID:        h.LastBlockID.ToProto(),
		ValidatorsHash:     h.ValidatorsHash,
		NextValidatorsHash: h.NextValidatorsHash,
		ConsensusHash:      h.ConsensusHash,
		AppHash:            h.AppHash,
		DataHash:           h.DataHash,
		EvidenceHash:       h.EvidenceHash,
		LastResultsHash:    h.LastResultsHash,
		LastCommitHash:     h.LastCommitHash,
		ProposerAddress:    h.ProposerAddress,
		TotalTxs:           h.TotalTxs,
		NumTxs:             h.NumTxs,
	}
}

// FromProto sets a protobuf Header to the given pointer.
// It returns an error if the header is invalid.
func HeaderFromProto(ph *tmproto.Header) (Header, error) {
	if ph == nil {
		return Header{}, errors.New("nil Header")
	}

	h := new(Header)

	bi, err := BlockIDFromProto(&ph.LastBlockID)
	if err != nil {
		return Header{}, err
	}

	h.Version = version.Consensus{Block: version.Protocol(ph.Version.Block), App: version.Protocol(ph.Version.App)}
	h.ChainID = ph.ChainID
	h.Height = ph.Height
	h.Time = ph.Time
	h.Height = ph.Height
	h.LastBlockID = *bi
	h.ValidatorsHash = ph.ValidatorsHash
	h.NextValidatorsHash = ph.NextValidatorsHash
	h.ConsensusHash = ph.ConsensusHash
	h.AppHash = ph.AppHash
	h.DataHash = ph.DataHash
	h.EvidenceHash = ph.EvidenceHash
	h.LastResultsHash = ph.LastResultsHash
	h.LastCommitHash = ph.LastCommitHash
	h.ProposerAddress = ph.ProposerAddress
	h.NumTxs = ph.NumTxs
	h.TotalTxs = ph.TotalTxs

	return *h, h.ValidateBasic()
}

//-------------------------------------

// CommitSig is a vote included in a Commit.
// For now, it is identical to a vote,
// but in the future it will contain fewer fields
// to eliminate the redundancy in commits.
// See https://github.com/tendermint/tendermint/issues/1648.
type CommitSig Vote

// String returns the underlying Vote.String()
func (cs *CommitSig) String() string {
	return cs.toVote().String()
}

// toVote converts the CommitSig to a vote.
// TODO: deprecate for #1648. Converting to Vote will require
// access to ValidatorSet.
func (cs *CommitSig) toVote() *Vote {
	if cs == nil {
		return nil
	}
	v := Vote(*cs)
	return &v
}

// ToProto converts CommitSig to protobuf
func (cs *CommitSig) ToProto() *tmproto.CommitSig {
	if cs == nil {
		return nil
	}

	return &tmproto.CommitSig{
		Type:             tmproto.SignedMsgType(cs.Type),
		Height:           cs.Height,
		Round:            int64(cs.Round),
		BlockID:          cs.BlockID.ToProto(),
		Timestamp:        cs.Timestamp,
		ValidatorAddress: cs.ValidatorAddress,
		ValidatorIndex:   int64(cs.ValidatorIndex),
		Signature:        cs.Signature,
	}
}

// FromProto sets a protobuf CommitSig to the given pointer.
// It returns an error if the CommitSig is invalid.
func (cs *CommitSig) FromProto(csp tmproto.CommitSig) error {

	blockID, err := BlockIDFromProto(&csp.BlockID)
	if err != nil {
		return err
	}

	cs.Type = SignedMsgType(csp.Type)
	cs.Height = csp.Height
	cs.Round = int(csp.Round)
	cs.BlockID = *blockID
	cs.Timestamp = csp.Timestamp
	cs.ValidatorAddress = csp.ValidatorAddress
	cs.ValidatorIndex = int(csp.ValidatorIndex)
	cs.Signature = csp.Signature

	return cs.ValidateBasic()
}

// ValidateBasic performs basic validation.
func (cs *CommitSig) ValidateBasic() error {
	if !IsVoteTypeValid(cs.Type) {
		return errors.New("invalid Type")
	}
	if cs.Height < 0 {
		return errors.New("negative Height")
	}
	if cs.Round < 0 {
		return errors.New("negative Round")
	}

	// NOTE: Timestamp validation is subtle and handled elsewhere.

	if err := cs.BlockID.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong BlockID: %v", err)
	}
	// BlockID.ValidateBasic would not err if we for instance have an empty hash but a
	// non-empty PartsSetHeader:
	if !cs.BlockID.IsZero() && !cs.BlockID.IsComplete() {
		return fmt.Errorf("blockID must be either empty or complete, got: %v", cs.BlockID)
	}
	if len(cs.ValidatorAddress) != crypto.AddressSize {
		return fmt.Errorf("expected ValidatorAddress size to be %d bytes, got %d bytes",
			crypto.AddressSize,
			len(cs.ValidatorAddress),
		)
	}
	if cs.ValidatorIndex < 0 {
		return errors.New("negative ValidatorIndex")
	}
	if len(cs.Signature) == 0 {
		return errors.New("signature is missing")
	}
	if len(cs.Signature) > MaxSignatureSize {
		return fmt.Errorf("signature is too big (max: %d)", MaxSignatureSize)
	}
	return nil
}

//-------------------------------------

// Commit contains the evidence that a block was committed by a set of validators.
// NOTE: Commit is empty for height 1, but never nil.
type Commit struct {
	// NOTE: The Precommits are in order of address to preserve the bonded ValidatorSet order.
	// Any peer with a block can gossip precommits by index with a peer without recalculating the
	// active ValidatorSet.
	BlockID    BlockID      `json:"block_id"`
	Precommits []*CommitSig `json:"precommits"`

	// memoized in first call to corresponding method
	// NOTE: can't memoize in constructor because constructor
	// isn't used for unmarshaling
	height   int64
	round    int
	hash     tmbytes.HexBytes
	bitArray *bits.BitArray
}

// NewCommit returns a new Commit with the given blockID and precommits.
// TODO: memoize ValidatorSet in constructor so votes can be easily reconstructed
// from CommitSig after #1648.
func NewCommit(blockID BlockID, precommits []*CommitSig) *Commit {
	return &Commit{
		BlockID:    blockID,
		Precommits: precommits,
	}
}

// Construct a VoteSet from the Commit and validator set. Panics
// if precommits from the commit can't be added to the voteset.
// Inverse of VoteSet.MakeCommit().
func CommitToVoteSet(chainID string, commit *Commit, vals *ValidatorSet) *VoteSet {
	height, round, typ := commit.Height(), commit.Round(), PrecommitType
	voteSet := NewVoteSet(chainID, height, round, typ, vals)
	for idx, precommit := range commit.Precommits {
		if precommit == nil {
			continue
		}
		added, err := voteSet.AddVote(commit.GetVote(idx))
		if !added || err != nil {
			panic(fmt.Sprintf("Failed to reconstruct LastCommit: %v", err))
		}
	}
	return voteSet
}

// GetVote converts the CommitSig for the given valIdx to a Vote.
// Returns nil if the precommit at valIdx is nil.
// Panics if valIdx >= commit.Size().
func (commit *Commit) GetVote(valIdx int) *Vote {
	commitSig := commit.Precommits[valIdx]
	if commitSig == nil {
		return nil
	}

	// NOTE: this commitSig might be for a nil blockID,
	// so we can't just use commit.BlockID here.
	// For #1648, CommitSig will need to indicate what BlockID it's for !
	blockID := commitSig.BlockID
	commit.memoizeHeightRound()
	return &Vote{
		Type:             PrecommitType,
		Height:           commit.height,
		Round:            commit.round,
		BlockID:          blockID,
		Timestamp:        commitSig.Timestamp,
		ValidatorAddress: commitSig.ValidatorAddress,
		ValidatorIndex:   valIdx,
		Signature:        commitSig.Signature,
	}
}

// VoteSignBytes constructs the SignBytes for the given CommitSig.
// The only unique part of the SignBytes is the Timestamp - all other fields
// signed over are otherwise the same for all validators.
// Panics if valIdx >= commit.Size().
func (commit *Commit) VoteSignBytes(chainID string, valIdx int) []byte {
	return commit.GetVote(valIdx).SignBytes(chainID)
}

// memoizeHeightRound memoizes the height and round of the commit using
// the first non-nil vote.
// Should be called before any attempt to access `commit.height` or `commit.round`.
func (commit *Commit) memoizeHeightRound() {
	if len(commit.Precommits) == 0 {
		return
	}
	if commit.height > 0 {
		return
	}
	for _, precommit := range commit.Precommits {
		if precommit != nil {
			commit.height = precommit.Height
			commit.round = precommit.Round
			return
		}
	}
}

// Height returns the height of the commit
func (commit *Commit) Height() int64 {
	commit.memoizeHeightRound()
	return commit.height
}

// Round returns the round of the commit
func (commit *Commit) Round() int {
	commit.memoizeHeightRound()
	return commit.round
}

// Type returns the vote type of the commit, which is always VoteTypePrecommit
// Implements VoteSetReader.
func (commit *Commit) Type() byte {
	return byte(PrecommitType)
}

// GetHeight returns height of the commit.
// Implements VoteSetReader.
func (commit *Commit) GetHeight() int64 {
	return commit.Height()
}

// GetRound returns height of the commit.
// Implements VoteSetReader.
func (commit *Commit) GetRound() int {
	return commit.Round()
}

// Size returns the number of signatures in the commit.
// Implements VoteSetReader.
func (commit *Commit) Size() int {
	if commit == nil {
		return 0
	}
	return len(commit.Precommits)
}

// BitArray returns a BitArray of which validators voted for BlockID or nil in this commit.
// Implements VoteSetReader.
func (commit *Commit) BitArray() *bits.BitArray {
	if commit.bitArray == nil {
		commit.bitArray = bits.NewBitArray(len(commit.Precommits))
		for i, precommit := range commit.Precommits {
			// TODO: need to check the BlockID otherwise we could be counting conflicts,
			// not just the one with +2/3 !
			commit.bitArray.SetIndex(i, precommit != nil)
		}
	}
	return commit.bitArray
}

// GetByIndex returns the vote corresponding to a given validator index.
// Panics if `index >= commit.Size()`.
// Implements VoteSetReader.
func (commit *Commit) GetByIndex(valIdx int) *Vote {
	return commit.GetVote(valIdx)
}

// IsCommit returns true if there is at least one signature.
// Implements VoteSetReader.
func (commit *Commit) IsCommit() bool {
	return len(commit.Precommits) != 0
}

// ValidateBasic performs basic validation that doesn't involve state data.
// Does not actually check the cryptographic signatures.
func (commit *Commit) ValidateBasic() error {
	if commit.Height() >= 1 {
		if commit.BlockID.IsZero() {
			return errors.New("Commit cannot be for nil block")
		}
		if len(commit.Precommits) == 0 {
			return errors.New("No precommits in commit")
		}
		height, round := commit.Height(), commit.Round()

		// Validate the precommits.
		for _, precommit := range commit.Precommits {
			// It's OK for precommits to be missing.
			if precommit == nil {
				continue
			}
			// Ensure that all votes are precommits.
			if precommit.Type != PrecommitType {
				return fmt.Errorf("Invalid commit vote. Expected precommit, got %v",
					precommit.Type)
			}
			// Ensure that all heights are the same.
			if precommit.Height != height {
				return fmt.Errorf("Invalid commit precommit height. Expected %v, got %v",
					height, precommit.Height)
			}
			// Ensure that all rounds are the same.
			if precommit.Round != round {
				return fmt.Errorf("Invalid commit precommit round. Expected %v, got %v",
					round, precommit.Round)
			}
		}
	}

	return nil
}

func (commit *Commit) ValidateBasicAndPower(blockID BlockID, chainID string, vals *ValidatorSet, talliedVotingPower int64) (error, int64) {
	if commit.Height() >= 1 {
		if commit.BlockID.IsZero() {
			return errors.New("Commit cannot be for nil block"), 0
		}
		if len(commit.Precommits) == 0 {
			return errors.New("No precommits in commit"), 0
		}
		height, round := commit.Height(), commit.Round()

		// Validate the precommits.
		for idx, precommit := range commit.Precommits {
			// It's OK for precommits to be missing.
			if precommit == nil {
				continue
			}
			// Ensure that all votes are precommits.
			if precommit.Type != PrecommitType {
				return fmt.Errorf("Invalid commit vote. Expected precommit, got %v",
					precommit.Type), 0
			}
			// Ensure that all heights are the same.
			if precommit.Height != height {
				return fmt.Errorf("Invalid commit precommit height. Expected %v, got %v",
					height, precommit.Height), 0
			}
			// Ensure that all rounds are the same.
			if precommit.Round != round {
				return fmt.Errorf("Invalid commit precommit round. Expected %v, got %v",
					round, precommit.Round), 0
			}

			// The vals and commit have a 1-to-1 correspondance.
			// This means we don't need the validator address or to do any lookup.
			val := vals.Validators[idx]

			// Validate signature.
			voteSignBytes := commit.VoteSignBytes(chainID, idx)
			if !val.PubKey.VerifyBytes(voteSignBytes, precommit.Signature) {
				return fmt.Errorf("wrong signature (#%d): %X", idx, precommit.Signature), 0
			}
			// Good!
			if blockID.Equals(commit.BlockID) {
				talliedVotingPower += val.VotingPower
			}
			// else {
			// It's OK that the BlockID doesn't match.  We include stray
			// signatures (~votes for nil) to measure validator availability.
			// }

		}
	}

	return nil, talliedVotingPower
}

// Hash returns the hash of the commit
func (commit *Commit) Hash() tmbytes.HexBytes {
	if commit == nil {
		return nil
	}
	if commit.hash == nil {
		bs := make([][]byte, len(commit.Precommits))
		for i, precommit := range commit.Precommits {
			bs[i] = cdcEncode(precommit)
		}
		commit.hash = merkle.SimpleHashFromByteSlices(bs)
	}
	return commit.hash
}

// StringIndented returns a string representation of the commit
func (commit *Commit) StringIndented(indent string) string {
	if commit == nil {
		return "nil-Commit"
	}
	precommitStrings := make([]string, len(commit.Precommits))
	for i, precommit := range commit.Precommits {
		precommitStrings[i] = precommit.String()
	}
	return fmt.Sprintf(`Commit{
%s  BlockID:    %v
%s  Precommits:
%s    %v
%s}#%v`,
		indent, commit.BlockID,
		indent,
		indent, strings.Join(precommitStrings, "\n"+indent+"    "),
		indent, commit.hash)
}

// ToProto converts Commit to protobuf
func (commit *Commit) ToProto() *tmproto.Commit {
	if commit == nil {
		return nil
	}

	c := new(tmproto.Commit)
	sigs := make([]tmproto.CommitSig, len(commit.Precommits))
	for i := range commit.Precommits {
		sigs[i] = *commit.Precommits[i].ToProto()
	}
	c.Precommits = sigs

	c.Height = commit.GetHeight()
	c.Round = int32(commit.GetRound())
	c.BlockID = commit.BlockID.ToProto()
	if commit.hash != nil {
		c.Hash = commit.hash
	}
	c.BitArray = commit.bitArray.ToProto()
	return c
}

// FromProto sets a protobuf Commit to the given pointer.
// It returns an error if the commit is invalid.
func CommitFromProto(cp *tmproto.Commit) (*Commit, error) {
	if cp == nil {
		return nil, errors.New("nil Commit")
	}

	var (
		commit   = new(Commit)
		bitArray *bits.BitArray
	)

	bi, err := BlockIDFromProto(&cp.BlockID)
	if err != nil {
		return nil, err
	}

	bitArray.FromProto(cp.BitArray)

	sigs := make([]*CommitSig, len(cp.Precommits))
	for i := range cp.Precommits {
		sigs[i] = &CommitSig{}
		if err := sigs[i].FromProto(cp.Precommits[i]); err != nil {
			return nil, err
		}
	}
	commit.Precommits = sigs

	commit.height = cp.GetHeight()
	commit.round = int(cp.GetRound())
	commit.BlockID = *bi
	commit.hash = cp.Hash
	commit.bitArray = bitArray

	return commit, commit.ValidateBasic()
}

//-----------------------------------------------------------------------------

// SignedHeader is a header along with the commits that prove it.
// It is the basis of the lite client.
type SignedHeader struct {
	*Header `json:"header"`

	Commit *Commit `json:"commit"`
}

// ValidateBasic does basic consistency checks and makes sure the header
// and commit are consistent.
// NOTE: This does not actually check the cryptographic signatures.  Make
// sure to use a Verifier to validate the signatures actually provide a
// significantly strong proof for this header's validity.
func (sh SignedHeader) ValidateBasic(chainID string) error {
	if sh.Header == nil {
		return errors.New("missing header")
	}
	if sh.Commit == nil {
		return errors.New("missing commit")
	}

	if err := sh.Header.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid header: %w", err)
	}
	if err := sh.Commit.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid commit: %w", err)
	}

	if sh.ChainID != chainID {
		return fmt.Errorf("header belongs to another chain %q, not %q", sh.ChainID, chainID)
	}

	// Make sure the header is consistent with the commit.
	if sh.Commit.Height() != sh.Height {
		return fmt.Errorf("header and commit height mismatch: %d vs %d", sh.Height, sh.Commit.Height())
	}
	if hhash, chash := sh.Hash(), sh.Commit.BlockID.Hash; !bytes.Equal(hhash, chash) {
		return fmt.Errorf("commit signs block %X, header is block %X", chash, hhash)
	}
	// ValidateBasic on the Commit.
	err := sh.Commit.ValidateBasic()
	if err != nil {
		return errors.Wrap(err, "commit.ValidateBasic failed during SignedHeader.ValidateBasic")
	}
	return nil
}

func (sh SignedHeader) String() string {
	return sh.StringIndented("")
}

// StringIndented returns a string representation of the SignedHeader.
func (sh SignedHeader) StringIndented(indent string) string {
	return fmt.Sprintf(`SignedHeader{
%s  %v
%s  %v
%s}`,
		indent, sh.Header.StringIndented(indent+"  "),
		indent, sh.Commit.StringIndented(indent+"  "),
		indent)
}

// ToProto converts SignedHeader to protobuf
func (sh *SignedHeader) ToProto() *tmproto.SignedHeader {
	if sh == nil {
		return nil
	}

	psh := new(tmproto.SignedHeader)
	if sh.Header != nil {
		psh.Header = sh.Header.ToProto()
	}
	if sh.Commit != nil {
		psh.Commit = sh.Commit.ToProto()
	}

	return psh
}

// FromProto sets a protobuf SignedHeader to the given pointer.
// It returns an error if the hader or the commit is invalid.
func SignedHeaderFromProto(shp *tmproto.SignedHeader) (*SignedHeader, error) {
	if shp == nil {
		return nil, errors.New("nil SignedHeader")
	}

	sh := new(SignedHeader)

	if shp.Header != nil {
		h, err := HeaderFromProto(shp.Header)
		if err != nil {
			return nil, err
		}
		sh.Header = &h
	}

	if shp.Commit != nil {
		c, err := CommitFromProto(shp.Commit)
		if err != nil {
			return nil, err
		}
		sh.Commit = c
	}

	return sh, nil
}

//-----------------------------------------------------------------------------

// Data contains the set of transactions included in the block
type Data struct {

	// Txs that will be applied by state @ block.Height+1.
	// NOTE: not all txs here are valid.  We're just agreeing on the order first.
	// This means that block.AppHash does not include these txs.
	Txs Txs `json:"txs"`

	// Volatile
	hash tmbytes.HexBytes
}

// Hash returns the hash of the data
func (data *Data) Hash() tmbytes.HexBytes {
	if data == nil {
		return (Txs{}).Hash()
	}
	if data.hash == nil {
		data.hash = data.Txs.Hash() // NOTE: leaves of merkle tree are TxIDs
	}
	return data.hash
}

// StringIndented returns a string representation of the transactions
func (data *Data) StringIndented(indent string) string {
	if data == nil {
		return "nil-Data"
	}
	txStrings := make([]string, tmmath.MinInt(len(data.Txs), 21))
	for i, tx := range data.Txs {
		if i == 20 {
			txStrings[i] = fmt.Sprintf("... (%v total)", len(data.Txs))
			break
		}
		txStrings[i] = fmt.Sprintf("%X (%d bytes)", tx.Hash(), len(tx))
	}
	return fmt.Sprintf(`Data{
%s  %v
%s}#%v`,
		indent, strings.Join(txStrings, "\n"+indent+"  "),
		indent, data.hash)
}

//-----------------------------------------------------------------------------

// EvidenceData contains any evidence of malicious wrong-doing by validators
type EvidenceData struct {
	Evidence EvidenceList `json:"evidence"`

	// Volatile
	hash tmbytes.HexBytes
}

// Hash returns the hash of the data.
func (data *EvidenceData) Hash() tmbytes.HexBytes {
	if data.hash == nil {
		data.hash = data.Evidence.Hash()
	}
	return data.hash
}

// StringIndented returns a string representation of the evidence.
func (data *EvidenceData) StringIndented(indent string) string {
	if data == nil {
		return "nil-Evidence"
	}
	evStrings := make([]string, tmmath.MinInt(len(data.Evidence), 21))
	for i, ev := range data.Evidence {
		if i == 20 {
			evStrings[i] = fmt.Sprintf("... (%v total)", len(data.Evidence))
			break
		}
		evStrings[i] = fmt.Sprintf("Evidence:%v", ev)
	}
	return fmt.Sprintf(`EvidenceData{
%s  %v
%s}#%v`,
		indent, strings.Join(evStrings, "\n"+indent+"  "),
		indent, data.hash)
}

//--------------------------------------------------------------------------------

// BlockID defines the unique ID of a block as its Hash and its PartSetHeader
type BlockID struct {
	Hash        tmbytes.HexBytes `json:"hash"`
	PartsHeader PartSetHeader    `json:"parts"`
}

// Equals returns true if the BlockID matches the given BlockID
func (blockID BlockID) Equals(other BlockID) bool {
	return bytes.Equal(blockID.Hash, other.Hash) &&
		blockID.PartsHeader.Equals(other.PartsHeader)
}

// Key returns a machine-readable string representation of the BlockID
func (blockID BlockID) Key() string {
	bz, err := cdc.MarshalBinaryBare(blockID.PartsHeader)
	if err != nil {
		panic(err)
	}
	return string(blockID.Hash) + string(bz)
}

// ValidateBasic performs basic validation.
func (blockID BlockID) ValidateBasic() error {
	// Hash can be empty in case of POLBlockID in Proposal.
	if err := ValidateHash(blockID.Hash); err != nil {
		return fmt.Errorf("wrong Hash")
	}
	if err := blockID.PartsHeader.ValidateBasic(); err != nil {
		return fmt.Errorf("wrong PartsHeader: %v", err)
	}
	return nil
}

// IsZero returns true if this is the BlockID of a nil block.
func (blockID BlockID) IsZero() bool {
	return len(blockID.Hash) == 0 &&
		blockID.PartsHeader.IsZero()
}

// IsComplete returns true if this is a valid BlockID of a non-nil block.
func (blockID BlockID) IsComplete() bool {
	return len(blockID.Hash) == tmhash.Size &&
		blockID.PartsHeader.Total > 0 &&
		len(blockID.PartsHeader.Hash) == tmhash.Size
}

// String returns a human readable string representation of the BlockID
func (blockID BlockID) String() string {
	return fmt.Sprintf(`%v:%v`, blockID.Hash, blockID.PartsHeader)
}

// ToProto converts BlockID to protobuf
func (blockID *BlockID) ToProto() tmproto.BlockID {
	if blockID == nil {
		return tmproto.BlockID{}
	}

	return tmproto.BlockID{
		Hash:        blockID.Hash,
		PartsHeader: blockID.PartsHeader.ToProto(),
	}
}

// FromProto sets a protobuf BlockID to the given pointer.
// It returns an error if the block id is invalid.
func BlockIDFromProto(bID *tmproto.BlockID) (*BlockID, error) {
	if bID == nil {
		return nil, errors.New("nil BlockID")
	}
	blockID := new(BlockID)
	ph, err := PartSetHeaderFromProto(&bID.PartsHeader)
	if err != nil {
		return nil, err
	}

	blockID.PartsHeader = *ph
	blockID.Hash = bID.Hash

	return blockID, blockID.ValidateBasic()
}
