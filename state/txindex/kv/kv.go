package kv

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/pubsub/query"
	tmstring "github.com/tendermint/tendermint/libs/strings"
	"github.com/tendermint/tendermint/state/txindex"
	"github.com/tendermint/tendermint/types"
)

const (
	tagKeySeparator = "/"
)

var _ txindex.TxIndexer = (*TxIndex)(nil)

// TxIndex is the simplest possible indexer, backed by key-value storage (levelDB).
type TxIndex struct {
	store                dbm.DB
	compositeKeysToIndex []string
	indexAllEvents       bool
}

// NewTxIndex creates new KV indexer.
func NewTxIndex(store dbm.DB, options ...func(*TxIndex)) *TxIndex {
	txi := &TxIndex{store: store, compositeKeysToIndex: make([]string, 0), indexAllEvents: false}
	for _, o := range options {
		o(txi)
	}
	return txi
}

// IndexEvents is an option for setting which composite keys to index.
func IndexEvents(compositeKeys []string) func(*TxIndex) {
	return func(txi *TxIndex) {
		txi.compositeKeysToIndex = compositeKeys
	}
}

// IndexAllEvents is an option for indexing all events.
func IndexAllEvents() func(*TxIndex) {
	return func(txi *TxIndex) {
		txi.indexAllEvents = true
	}
}

// Get gets transaction from the TxIndex storage and returns it or nil if the
// transaction is not found.
func (txi *TxIndex) Get(hash []byte) (*types.TxResult, error) {
	if len(hash) == 0 {
		return nil, txindex.ErrorEmptyHash
	}

	rawBytes, _ := txi.store.Get(hash)
	if rawBytes == nil {
		return nil, nil
	}

	txResult := new(types.TxResult)
	err := cdc.UnmarshalBinaryBare(rawBytes, &txResult)
	if err != nil {
		return nil, fmt.Errorf("error reading TxResult: %v", err)
	}

	return txResult, nil
}

// AddBatch indexes a batch of transactions using the given list of events. Each
// key that indexed from the tx's events is a composite of the event type and
// the respective attribute's key delimited by a "." (eg. "account.number").
// Any event with an empty type is not indexed.
func (txi *TxIndex) AddBatch(b *txindex.Batch) error {
	storeBatch := txi.store.NewBatch()
	defer storeBatch.Close()

	for _, result := range b.Ops {
		hash := result.Tx.Hash()

		// index tx by events
		txi.indexEvents(result, hash, storeBatch)

		// index tx by height
		if txi.indexAllEvents || tmstring.StringInSlice(types.TxHeightKey, txi.compositeKeysToIndex) {
			storeBatch.Set(keyForHeight(result), hash)
		}

		// index tx by hash
		rawBytes, err := cdc.MarshalBinaryBare(result)
		if err != nil {
			return err
		}
		storeBatch.Set(hash, rawBytes)
	}

	storeBatch.WriteSync()
	return nil
}

// Index indexes a single transaction using the given list of events. Each key
// that indexed from the tx's events is a composite of the event type and the
// respective attribute's key delimited by a "." (eg. "account.number").
// Any event with an empty type is not indexed.
func (txi *TxIndex) Index(result *types.TxResult) error {
	b := txi.store.NewBatch()
	defer b.Close()

	hash := result.Tx.Hash()

	// index tx by events
	txi.indexEvents(result, hash, b)

	// index tx by height
	if txi.indexAllEvents || tmstring.StringInSlice(types.TxHeightKey, txi.compositeKeysToIndex) {
		b.Set(keyForHeight(result), hash)
	}

	// index tx by hash
	rawBytes, err := cdc.MarshalBinaryBare(result)
	if err != nil {
		return err
	}

	b.Set(hash, rawBytes)
	b.WriteSync()

	return nil
}

func (txi *TxIndex) indexEvents(result *types.TxResult, hash []byte, store dbm.SetDeleter) {
	for _, event := range result.Result.Events {
		// only index events with a non-empty type
		if len(event.Type) == 0 {
			continue
		}

		for _, attr := range event.Attributes {
			if len(attr.Key) == 0 {
				continue
			}

			compositeTag := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			if txi.indexAllEvents || tmstring.StringInSlice(compositeTag, txi.compositeKeysToIndex) {
				store.Set(keyForEvent(compositeTag, attr.Value, result), hash)
			}
		}
	}
}

func (txi *TxIndex) deleteEvents(result *types.TxResult, hash []byte, store dbm.SetDeleter) {
	for _, event := range result.Result.Events {
		// only index events with a non-empty type
		if len(event.Type) == 0 {
			continue
		}

		for _, attr := range event.Attributes {
			if len(attr.Key) == 0 {
				continue
			}

			compositeTag := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			//if txi.indexAllTags || cmn.StringInSlice(compositeTag, txi.tagsToIndex) { // defensive
			store.Delete(keyForEvent(compositeTag, attr.Value, result))
			//}
		}
	}
}

func (txi *TxIndex) DeleteFromHeight(ctx context.Context, height int64) error {
	q, err := query.New("tx.height > " + strconv.Itoa(int(height)))
	if err != nil {
		return err
	}
	res, _, err := txi.Search(ctx, q)
	if err != nil {
		return err
	}
	b := txi.store.NewBatch()
	defer b.Close()
	for _, txRes := range res {
		hash := txRes.Tx.Hash()
		// index tx by events
		txi.deleteEvents(txRes, hash, b)
		// index tx by height
		if txi.indexAllEvents || tmstring.StringInSlice(types.TxHeightKey, txi.compositeKeysToIndex) {
			b.Delete(keyForHeight(txRes))
		}
		b.Delete(hash)
	}
	b.WriteSync()
	return nil
}

// Search performs a search using the given query. It breaks the query into
// conditions (like "tx.height > 5"). For each condition, it queries the DB
// index. One special use cases here: (1) if "tx.hash" is found, it returns tx
// result for it (2) for range queries it is better for the client to provide
// both lower and upper bounds, so we are not performing a full scan. Results
// from querying indexes are then intersected and returned to the caller.
func (txi *TxIndex) Search(ctx context.Context, q *query.Query) ([]*types.TxResult, int, error) {
	var hashesInitialized bool
	filteredHashes := make(map[string]*keyAndHash)

	// get a list of conditions (like "tx.height > 5")
	conditions, err := q.Conditions()
	if err != nil {
		return nil, 0, errors.Wrap(err, "error during parsing conditions from query")
	}

	// if there is a hash condition, return the result immediately
	hash, ok, err := lookForHash(conditions...)
	if err != nil {
		return nil, 0, errors.Wrap(err, "error during searching for a hash in the query")
	} else if ok {
		res, err := txi.Get(hash)
		switch {
		case err != nil:
			return []*types.TxResult{}, 0, errors.Wrap(err, "error while retrieving the result")
		case res == nil:
			return []*types.TxResult{}, 0, nil
		default:
			return []*types.TxResult{res}, 0, nil
		}
	}

	// conditions to skip because they're handled before "everything else"
	skipIndexes := make([]int, 0)

	// extract ranges
	// if both upper and lower bounds exist, it's better to get them in order not
	// no iterate over kvs that are not within range.
	ranges, rangeIndexes := lookForRanges(conditions...)
	if len(ranges) > 0 {
		skipIndexes = append(skipIndexes, rangeIndexes...)

		for _, r := range ranges {
			if !hashesInitialized {
				filteredHashes = txi.matchRange(ctx, r, startKey(r.key), filteredHashes, true)
				hashesInitialized = true

				// Ignore any remaining conditions if the first condition resulted
				// in no matches (assuming implicit AND operand).
				if len(filteredHashes) == 0 {
					break
				}
			} else {
				filteredHashes = txi.matchRange(ctx, r, startKey(r.key), filteredHashes, false)
			}
		}
	}

	// if there is a height condition ("tx.height=3"), extract it
	height := lookForHeight(conditions...)

	// for all other conditions
	for i, c := range conditions {
		if intInSlice(i, skipIndexes) {
			continue
		}
		if !hashesInitialized {
			filteredHashes = txi.match(ctx, c, startKeyForCondition(c, height), filteredHashes, true)
			hashesInitialized = true

			// Ignore any remaining conditions if the first condition resulted
			// in no matches (assuming implicit AND operand).
			if len(filteredHashes) == 0 {
				break
			}
		} else {
			filteredHashes = txi.match(ctx, c, startKeyForCondition(c, height), filteredHashes, false)
		}
	}

	results := make([]*types.TxResult, 0, len(filteredHashes))
	if q.Pagination != nil {
		var sortHashes []*keyAndHash
		for _, hashKeyPair := range filteredHashes {
			sortHashes = append(sortHashes, hashKeyPair)
		}
		filteredHashes = nil // IGNORE MAP PLEASE!! XD

		switch q.Pagination.Sort {
		case "desc":
			sort.Slice(sortHashes, func(i, j int) bool {
				a := strings.Split(sortHashes[i].key, "/")
				b := strings.Split(sortHashes[j].key, "/")
				aHeight, _ := strconv.Atoi(a[2])
				bHeight, _ := strconv.Atoi(b[2])
				if aHeight == bHeight {
					aIndex, _ := strconv.Atoi(a[3])
					bIndex, _ := strconv.Atoi(b[3])
					return aIndex < bIndex
				}
				return aHeight > bHeight
			})
		case "asc", "":
			sort.Slice(sortHashes, func(i, j int) bool {
				a := strings.Split(sortHashes[i].key, "/")
				b := strings.Split(sortHashes[j].key, "/")
				aHeight, _ := strconv.Atoi(a[2])
				bHeight, _ := strconv.Atoi(b[2])
				if aHeight == bHeight {
					aIndex, _ := strconv.Atoi(a[3])
					bIndex, _ := strconv.Atoi(b[3])
					return aIndex < bIndex
				}
				return aHeight < bHeight
			})
		}
		skipCount := 0
		results = make([]*types.TxResult, 0, q.Pagination.Size)
		for _, hat := range sortHashes {
			select {
			case <-ctx.Done():
				break
			default:
				// skip keys
				if skipCount > q.Pagination.Skip {
					skipCount++
					continue
				}
				res, err := txi.Get(hat.hash)
				if err != nil {
					return nil, 0, errors.Wrapf(err, "failed to get Tx{%X}", hat.hash)
				}
				results = append(results, res)
				// Potentially exit early.
				if len(results) == cap(results) {
					return results, 0, nil
				}
			}
		}
	}
	for _, hashAndKeyPair := range filteredHashes {
		// Potentially exit early.
		select {
		case <-ctx.Done():
			break
		default:
			res, err := txi.Get(hashAndKeyPair.hash)
			if err != nil {
				return nil, 0, errors.Wrapf(err, "failed to get Tx{%X}", hashAndKeyPair.hash)
			}
			results = append(results, res)
		}
	}
	return results, 0, nil
}

func lookForHash(conditions ...query.Condition) (hash []byte, ok bool, err error) {
	for _, c := range conditions {
		if c.CompositeKey == types.TxHashKey {
			decoded, err := hex.DecodeString(c.Operand.(string))
			return decoded, true, err
		}
	}
	return
}

// lookForHeight returns a height if there is an "height=X" condition.
func lookForHeight(conditions ...query.Condition) (height int64) {
	for _, c := range conditions {
		if c.CompositeKey == types.TxHeightKey && c.Op == query.OpEqual {
			return c.Operand.(int64)
		}
	}
	return 0
}

// special map to hold range conditions
// Example: account.number => queryRange{lowerBound: 1, upperBound: 5}
type queryRanges map[string]queryRange

type queryRange struct {
	lowerBound        interface{} // int || time.Time
	upperBound        interface{} // int || time.Time
	key               string
	includeLowerBound bool
	includeUpperBound bool
}

func (r queryRange) lowerBoundValue() interface{} {
	if r.lowerBound == nil {
		return nil
	}

	if r.includeLowerBound {
		return r.lowerBound
	}

	switch t := r.lowerBound.(type) {
	case int64:
		return t + 1
	case time.Time:
		return t.Unix() + 1
	default:
		panic("not implemented")
	}
}

func (r queryRange) AnyBound() interface{} {
	if r.lowerBound != nil {
		return r.lowerBound
	}

	return r.upperBound
}

func (r queryRange) upperBoundValue() interface{} {
	if r.upperBound == nil {
		return nil
	}

	if r.includeUpperBound {
		return r.upperBound
	}

	switch t := r.upperBound.(type) {
	case int64:
		return t - 1
	case time.Time:
		return t.Unix() - 1
	default:
		panic("not implemented")
	}
}

func lookForRanges(conditions ...query.Condition) (ranges queryRanges, indexes []int) {
	ranges = make(queryRanges)
	for i, c := range conditions {
		if isRangeOperation(c.Op) {
			r, ok := ranges[c.CompositeKey]
			if !ok {
				r = queryRange{key: c.CompositeKey}
			}
			switch c.Op {
			case query.OpGreater:
				r.lowerBound = c.Operand
			case query.OpGreaterEqual:
				r.includeLowerBound = true
				r.lowerBound = c.Operand
			case query.OpLess:
				r.upperBound = c.Operand
			case query.OpLessEqual:
				r.includeUpperBound = true
				r.upperBound = c.Operand
			}
			ranges[c.CompositeKey] = r
			indexes = append(indexes, i)
		}
	}
	return ranges, indexes
}

func isRangeOperation(op query.Operator) bool {
	switch op {
	case query.OpGreater, query.OpGreaterEqual, query.OpLess, query.OpLessEqual:
		return true
	default:
		return false
	}
}

type hashAndIndexer struct {
	hash []byte
	txi  *TxIndex
}

type sortByHeight []hashAndIndexer
type sortByKey []hashAndIndexer

type keyAndHash struct {
	key  string
	hash []byte
}

// Retrieves the keys from the iterator based on condition
// NOTE: filteredHashes may be empty if no previous condition has matched.
func (txi *TxIndex) keys(
	ctx context.Context,
	c query.Condition,
	startKeyBz []byte,
) map[string][]byte {
	hashes := make(map[string][]byte)
	switch {
	case c.Op == query.OpEqual:
		it, _ := dbm.IteratePrefix(txi.store, startKeyBz)
		defer it.Close()

		for ; it.Valid(); it.Next() {
			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
				hashes[string(it.Key())] = it.Value()
			}

		}
	case c.Op == query.OpContains:
		// XXX: startKey does not apply here.
		// For example, if startKey = "account.owner/an/" and search query = "account.owner CONTAINS an"
		// we can't iterate with prefix "account.owner/an/" because we might miss keys like "account.owner/Ulan/"
		it, _ := dbm.IteratePrefix(txi.store, startKey(c.CompositeKey))
		defer it.Close()

		for ; it.Valid(); it.Next() {
			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
				if !isTagKey(it.Key()) {
					continue
				}

				if strings.Contains(extractValueFromKey(it.Key()), c.Operand.(string)) {
					hashes[string(it.Key())] = it.Value()
				}
			}
		}
	default:
		panic("other operators should be handled already")
	}
	return hashes
}

// match returns all matching txs by hash that meet a given condition and start
// key. An already filtered result (filteredHashes) is provided such that any
// non-intersecting matches are removed.
//
// NOTE: filteredHashes may be empty if no previous condition has matched.
func (txi *TxIndex) match(
	ctx context.Context,
	c query.Condition,
	startKeyBz []byte,
	filteredHashes map[string]*keyAndHash,
	firstRun bool,
) map[string]*keyAndHash {
	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHashes) == 0 {
		return filteredHashes
	}
	tmpHashes := make(map[string]*keyAndHash)
	switch {
	case c.Op == query.OpEqual:
		it, _ := dbm.IteratePrefix(txi.store, startKeyBz)
		defer it.Close()

		for ; it.Valid(); it.Next() {
			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
				key := string(it.Value())
				tmpHashes[key] = &keyAndHash{string(it.Key()), it.Value()}
			}

		}
	case c.Op == query.OpContains:
		// XXX: startKey does not apply here.
		// For example, if startKey = "account.owner/an/" and search query = "account.owner CONTAINS an"
		// we can't iterate with prefix "account.owner/an/" because we might miss keys like "account.owner/Ulan/"
		it, _ := dbm.IteratePrefix(txi.store, startKey(c.CompositeKey))
		defer it.Close()

		for ; it.Valid(); it.Next() {
			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
				if !isTagKey(it.Key()) {
					continue
				}

				if strings.Contains(extractValueFromKey(it.Key()), c.Operand.(string)) {
					tmpHashes[string(it.Value())] = &keyAndHash{string(it.Key()), it.Value()}
				}
			}
		}
	default:
		panic("other operators should be handled already")
	}

	if len(tmpHashes) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHashes
	}

	// Remove/reduce matches in filteredHashes that were not found in this
	// match (tmpHashes).
	for k := range filteredHashes {
		if tmpHashes[k] == nil {
			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
				delete(filteredHashes, k)
			}
		}
	}

	return filteredHashes
}

// matchRange returns all matching txs by hash that meet a given queryRange and
// start key. An already filtered result (filteredHashes) is provided such that
// any non-intersecting matches are removed.
//
// NOTE: filteredHashes may be empty if no previous condition has matched.
func (txi *TxIndex) matchRange(
	ctx context.Context,
	r queryRange,
	startKey []byte,
	filteredHashes map[string]*keyAndHash,
	firstRun bool,
) map[string]*keyAndHash {
	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHashes) == 0 {
		return filteredHashes
	}

	tmpHashes := make(map[string]*keyAndHash)
	lowerBound := r.lowerBoundValue()
	upperBound := r.upperBoundValue()

	it, _ := dbm.IteratePrefix(txi.store, startKey)
	defer it.Close()

LOOP:
	for ; it.Valid(); it.Next() {
		if !isTagKey(it.Key()) {
			continue
		}

		if _, ok := r.AnyBound().(int64); ok {
			v, err := strconv.ParseInt(extractValueFromKey(it.Key()), 10, 64)
			if err != nil {
				continue LOOP
			}

			include := true
			if lowerBound != nil && v < lowerBound.(int64) {
				include = false
			}

			if upperBound != nil && v > upperBound.(int64) {
				include = false
			}

			if include {
				tmpHashes[string(it.Value())] = &keyAndHash{string(it.Key()), it.Value()}
			}

			// XXX: passing time in a ABCI Events is not yet implemented
			// case time.Time:
			// 	v := strconv.ParseInt(extractValueFromKey(it.Key()), 10, 64)
			// 	if v == r.upperBound {
			// 		break
			// 	}
		}

		// Potentially exit early.
		select {
		case <-ctx.Done():
			break
		default:
		}
	}

	if len(tmpHashes) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHashes
	}

	// Remove/reduce matches in filteredHashes that were not found in this
	// match (tmpHashes).
	for k := range filteredHashes {
		if tmpHashes[k] == nil {
			delete(filteredHashes, k)

			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
			}
		}
	}

	return filteredHashes
}

///////////////////////////////////////////////////////////////////////////////
// Keys

func isTagKey(key []byte) bool {
	return strings.Count(string(key), tagKeySeparator) == 3
}

func extractValueFromKey(key []byte) string {
	parts := strings.SplitN(string(key), tagKeySeparator, 3)
	return parts[1]
}

func keyForEvent(key string, value []byte, result *types.TxResult) []byte {
	return []byte(fmt.Sprintf("%s/%s/%d/%d",
		key,
		value,
		result.Height,
		result.Index,
	))
}

func keyForHeight(result *types.TxResult) []byte {
	return []byte(fmt.Sprintf("%s/%d/%d/%d",
		types.TxHeightKey,
		result.Height,
		result.Height,
		result.Index,
	))
}

func startKeyForCondition(c query.Condition, height int64) []byte {
	if height > 0 {
		return startKey(c.CompositeKey, c.Operand, height)
	}
	return startKey(c.CompositeKey, c.Operand)
}

func startKey(fields ...interface{}) []byte {
	var b bytes.Buffer
	for _, f := range fields {
		b.Write([]byte(fmt.Sprintf("%v", f) + tagKeySeparator))
	}
	return b.Bytes()
}
