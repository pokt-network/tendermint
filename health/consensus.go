package health

import (
	"time"

	"github.com/tendermint/tendermint/consensus/types"
	tm "github.com/tendermint/tendermint/types"
)

type Round struct {
	Proposer        tm.Address
	RoundNumber     int64
	MaxStep         types.RoundStepType
	ConsensusTiming ConsensusTiming
	PreVotes        VoteMetrics
	PreCommits      VoteMetrics
}

type ConsensusMetrics struct {
	Rounds map[int64]Round
}

type ConsensusTiming struct {
	ProposeTime   int64
	PreVoteTime   int64
	PreCommitTime int64
}

func (hm *HealthMetrics) InitRound(height int64, roundNumber int64) {
	bm := hm.BlockMetrics[height]
	if bm.ConsensusMetrics.Rounds == nil {
		bm.ConsensusMetrics.Rounds = make(map[int64]Round)
	}
	if _, found := bm.ConsensusMetrics.Rounds[roundNumber]; found {
		return
	}
	bm.ConsensusMetrics.Rounds[roundNumber] = Round{
		RoundNumber: roundNumber,
		PreVotes:    VoteMetrics{Voters: make([]Voter, 0)},
		PreCommits:  VoteMetrics{Voters: make([]Voter, 0)},
	}
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) SetProposer(height, round int64, address tm.Address) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	hm.InitRound(height, round)
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.Proposer = address
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) SetProposeTime(height, round int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	hm.InitRound(height, round)
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.ConsensusTiming.ProposeTime = d.Milliseconds()
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) SetPreVoteTime(height, round int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	hm.InitRound(height, round)
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.ConsensusTiming.PreVoteTime = d.Milliseconds()
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) SetPreCommitTime(height, round int64, d time.Duration) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	hm.InitRound(height, round)
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.ConsensusTiming.PreCommitTime = d.Milliseconds()
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) SetStep(height, round int64, s types.RoundStepType) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	hm.InitRound(height, round)
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.MaxStep = s
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddVote(vote tm.Vote) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(vote.Height)
	hm.InitRound(vote.Height, int64(vote.Round))
	voteString := ""
	if !vote.BlockID.IsZero() {
		voteString = vote.String()
	}
	voter := Voter{
		Validator: Validator{
			Address: vote.ValidatorAddress,
		},
		Vote: voteString,
	}
	if vote.Type == tm.PrevoteType {
		hm.AddPreVote(vote.Height, int64(vote.Round), voter)
	} else {
		hm.AddPreCommit(vote.Height, int64(vote.Round), voter)
	}
}

func (hm *HealthMetrics) AddPreVote(height, round int64, v Voter) {
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.PreVotes.AddVoter(v)
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) AddPreCommit(height, round int64, v Voter) {
	bm := hm.BlockMetrics[height]
	r := bm.ConsensusMetrics.Rounds[round]
	r.PreCommits.AddVoter(v)
	bm.ConsensusMetrics.Rounds[round] = r
	hm.BlockMetrics[height] = bm
}

func (hm *HealthMetrics) GetConsensusMetrics(height int64) ConsensusMetrics {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	return hm.BlockMetrics[height].ConsensusMetrics
}

func (hm *HealthMetrics) SetConsensusMetrics(height int64, cm ConsensusMetrics) {
	hm.mtx.Lock()
	defer hm.mtx.Unlock()
	hm.InitHeight(height)
	bm := hm.BlockMetrics[height]
	bm.ConsensusMetrics = cm
	hm.BlockMetrics[height] = bm
}

type Validator struct {
	Address    tm.Address
	ServiceURL string
	Power      int64
}

type Voter struct {
	Validator
	Vote string
}

type VoteMetrics struct {
	TotalVoters    int64
	TotalNilVoters int64
	Voters         []Voter
}

func (vm *VoteMetrics) AddVoter(voter Voter) {
	vm.TotalVoters++
	if voter.Vote == "" {
		vm.TotalNilVoters++
	}
	vm.Voters = append(vm.Voters, voter)
}
