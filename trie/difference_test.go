// --- Start fork code ---
package trie

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// data represents the test data for the trie comparison test.
// It contains the actual data generated from some Ethereum mainnet block.
// This data is used to test the CompareTrie function
// by comparing the pre-state MPT and post-state MPT and verifying the MPT diffs matches the StateDiffs.
type data struct {
	PreRoot    common.Hash      `json:"preRoot"`    // Pre-state root
	PostRoot   common.Hash      `json:"postRoot"`   // Post-state root
	PreState   []hexutil.Bytes  `json:"prestate"`   // Pre-state MPT nodes
	Committed  []hexutil.Bytes  `json:"committed"`  // Committed state MPT nodes
	AccessList types.AccessList `json:"accessList"` // Access list
	StateDiffs []*StateDiff     `json:"stateDiffs"` // State diffs
}

// StateDiff represents a difference in the state of an account.
type StateDiff struct {
	Address     common.Address `json:"address"`               // Address of the account
	PreAccount  *Account       `json:"preAccount,omitempty"`  // Pre-state account
	PostAccount *Account       `json:"postAccount,omitempty"` // Post-state account
	Storage     []*StorageDiff `json:"storage,omitempty"`     // Storage diffs
}

// Account represents an account in the state.
type Account struct {
	Balance     *big.Int    `json:"balance"`     // Balance of the account
	CodeHash    common.Hash `json:"codeHash"`    // Code hash of the account
	Nonce       uint64      `json:"nonce"`       // Nonce of the account
	StorageHash common.Hash `json:"storageHash"` // Storage hash of the account
}

// StorageDiff represents a difference in the storage of an account.
type StorageDiff struct {
	Slot      common.Hash `json:"storageKey"`          // Storage key
	PreValue  common.Hash `json:"preValue,omitempty"`  // Pre-state storage value
	PostValue common.Hash `json:"postValue,omitempty"` // Post-state storage value
}

func loadTestData(t *testing.T, testCase string) *data {
	d := new(data)
	f, err := os.Open(filepath.Join("testdata", testCase))
	require.NoError(t, err)
	defer f.Close()

	err = json.NewDecoder(f).Decode(d)
	require.NoError(t, err)

	return d
}

func TestCompareTrie(t *testing.T) {
	testCases := []string{
		"diffs_22068970.json",
		"diffs_22069129.json",
		"diffs_22069130.json",
		"diffs_22069131.json",
		"diffs_22069132.json",
		"diffs_22069133.json",
		"diffs_22069134.json",
		"diffs_22069135.json",
		"diffs_22069136.json",
		"diffs_22069137.json",
		"diffs_22069138.json",
		"diffs_22069139.json",
	}

	for _, testCase := range testCases {
		t.Run(testCase, func(t *testing.T) {
			d := loadTestData(t, testCase)
			testCompareTrie(t, d)
		})
	}
}

func testCompareTrie(t *testing.T, d *data) {
	proofDB := memorydb.New()
	for _, n := range d.PreState {
		proofDB.Put(crypto.Keccak256(n), n)
	}

	for _, n := range d.Committed {
		proofDB.Put(crypto.Keccak256(n), n)
	}

	// --- 1. Validate the StateDiffs data is contained in the pre-state and post-state MPT ---
	for _, stateDiff := range d.StateDiffs {
		// Validate pre-state account is present in the pre-state MPT
		assertVerifyAccount(t, d.PreRoot, stateDiff.Address, proofDB, stateDiff.PreAccount)

		// Validate post-state account is present in the post-state MPT
		assertVerifyAccount(t, d.PostRoot, stateDiff.Address, proofDB, stateDiff.PostAccount)

		// Validate Storage diffs
		if len(stateDiff.Storage) > 0 {
			for _, storageDiff := range stateDiff.Storage {
				// Validate pre-state storage value is contained in the pre-state MPT
				if stateDiff.PreAccount == nil {
					// If the pre-state account is nil, the pre-state storage value should be nil
					assertStorageValue(t, storageDiff.PreValue.Bytes(), (common.Hash{}))
				} else {
					// If the pre-state account is not nil, the pre-state storage value should be contained in the pre-state MPT
					assertVerifyStorage(t, stateDiff.PreAccount.StorageHash, storageDiff.Slot, proofDB, storageDiff.PreValue)
				}

				// Validate post-state storage value is contained in the post-state MPT
				if stateDiff.PostAccount == nil {
					assertStorageValue(t, storageDiff.PostValue.Bytes(), (common.Hash{}))
				} else {
					// If the post-state account is not nil, the post-state storage value should be contained in the post-state MPT
					assertVerifyStorage(t, stateDiff.PostAccount.StorageHash, storageDiff.Slot, proofDB, storageDiff.PostValue)
				}
			}
		}
	}

	// --- 2. Test CompareTrie for account state ---
	stateMPTDiffs := requireCompareTrie(t, d.PreRoot, d.PostRoot, proofDB)
	require.Len(t, stateMPTDiffs, len(d.StateDiffs), "The number of Account State diffs and MPT diffs should be equal")

	for _, dataDiff := range d.StateDiffs {
		// Validate the expected StateDiff is present in the MPT diffs
		hash := common.BytesToHash(crypto.Keccak256(dataDiff.Address.Bytes()))
		mptDiff, ok := stateMPTDiffs[hash.Hex()]
		require.Truef(t, ok, "StateDiff not found in MPT diffs for address %v (hash: %v)", dataDiff.Address.Hex(), hash.Hex())

		// Validate the pre-state and post-state account values in the MPT diff match expected StateDiff
		assertAccountValue(t, mptDiff.LeftValue, dataDiff.PreAccount)
		assertAccountValue(t, mptDiff.RightValue, dataDiff.PostAccount)

		// --- 3. Test CompareTrie for storage ---
		preStorageHash := types.EmptyRootHash
		if dataDiff.PreAccount != nil {
			preStorageHash = dataDiff.PreAccount.StorageHash
		}

		postStorageHash := types.EmptyRootHash
		if dataDiff.PostAccount != nil {
			postStorageHash = dataDiff.PostAccount.StorageHash
		}

		storageMPTDiffs := requireCompareTrie(t, preStorageHash, postStorageHash, proofDB)
		require.Equal(t, len(dataDiff.Storage), len(storageMPTDiffs), "The number of storage diffs should be equal to the number of diffs returned by CompareTrie")

		for _, storageDiff := range dataDiff.Storage {
			hash := common.BytesToHash(crypto.Keccak256(storageDiff.Slot.Bytes()))
			mptDiff, ok := storageMPTDiffs[hash.Hex()]
			require.Truef(t, ok, "StorageDiff not found in MPT diffs for slot %v (hash: %v)", storageDiff.Slot.Hex(), hash.Hex())

			assertStorageValue(t, mptDiff.LeftValue, storageDiff.PreValue)
			assertStorageValue(t, mptDiff.RightValue, storageDiff.PostValue)
		}
	}
}

// --- End fork code ---

func requireCompareTrie(t *testing.T, preRoot, postRoot common.Hash, proofDB ethdb.KeyValueReader) map[string]*Diff {
	diffs, err := ComputeTrieDiff(preRoot, postRoot, proofDB)
	require.NoError(t, err)

	diffsMap := make(map[string]*Diff)
	for _, diff := range diffs {
		require.Len(t, diff.Key, 32, "Key length should be 32 bytes")
		hash := common.BytesToHash(diff.Key)
		diffsMap[hash.Hex()] = diff
	}

	return diffsMap
}

func assertVerifyAccount(t *testing.T, root common.Hash, addr common.Address, proofDB ethdb.KeyValueReader, expectedAccount *Account) {
	value, err := VerifyProof(root, crypto.Keccak256(addr.Bytes()), proofDB)
	require.NoErrorf(t, err, "VerifyProof at %v failed for account %v", root.Hex(), addr.Hex())
	assertAccountValue(t, value, expectedAccount)
}

func assertAccountValue(t *testing.T, value []byte, expectedAccount *Account) {
	if expectedAccount == nil {
		assert.Len(t, value, 0, "Expected account to be nil")
		return
	}

	actualAccount := new(types.StateAccount)
	err := rlp.DecodeBytes(value, actualAccount)
	require.NoErrorf(t, err, "RLP decode failed for account")

	assert.Equal(t, expectedAccount.Balance.Uint64(), actualAccount.Balance.Uint64())
	assert.Equal(t, expectedAccount.CodeHash.Hex(), common.BytesToHash(actualAccount.CodeHash).Hex())
	assert.Equal(t, expectedAccount.Nonce, actualAccount.Nonce)
	assert.Equal(t, expectedAccount.StorageHash.Hex(), actualAccount.Root.Hex())
}

func assertVerifyStorage(t *testing.T, root common.Hash, slot common.Hash, proofDB ethdb.KeyValueReader, expectedValue common.Hash) {
	value, err := VerifyProof(root, crypto.Keccak256(slot.Bytes()), proofDB)
	require.NoErrorf(t, err, "VerifyProof at %v failed for storage slot %v", root.Hex(), slot.Hex())

	assertStorageValue(t, value, expectedValue)
}

func assertStorageValue(t *testing.T, value []byte, expectedValue common.Hash) {
	if expectedValue == (common.Hash{}) {
		assert.Equal(t, expectedValue, common.BytesToHash(value), "Expected storage value to be nil")
		return
	}

	_, content, _, err := rlp.Split(value[:])
	require.NoError(t, err, "RLP decode failed for storage value")
	assert.Equal(t, expectedValue, common.BytesToHash(content), "Storage value mismatch")
}
