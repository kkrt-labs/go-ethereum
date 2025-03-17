// --- Start fork code ---
package trie

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

type Diff struct {
	Key        []byte // The key of the value that differs
	LeftValue  []byte // The value at the given key in the left trie
	RightValue []byte // The value at the given key in the right trie
}

func (d *Diff) MarshalJSON() ([]byte, error) {
	type DiffJSON struct {
		Key        hexutil.Bytes `json:"key"`
		LeftValue  hexutil.Bytes `json:"leftValue"`
		RightValue hexutil.Bytes `json:"rightValue"`
	}
	return json.Marshal(DiffJSON{
		Key:        hexutil.Bytes(d.Key),
		LeftValue:  hexutil.Bytes(d.LeftValue),
		RightValue: hexutil.Bytes(d.RightValue),
	})
}

var branchIndices = []byte{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf}

// ComputeTrieDiff compares two tries and returns a list of all the value that differs between the two
// with their key and the value in the left and right trie.
//
// - left is the root hash of the left trie
// - right is the root hash of the right trie
//
// - proofDB is a database of MPT nodes keyed by their hash that minimally contains all the intermediate nodes
// that differ between the two tries.
//
// This function is particularly helpful to compute the differences between pre-state and post-state
// following a state transition.
func ComputeTrieDiff(left, right common.Hash, proofDB ethdb.KeyValueReader) ([]*Diff, error) {
	diffs, err := computeTrieDiff(hashNode(left[:]), hashNode(right[:]), proofDB)
	if err != nil {
		return nil, err
	}

	for _, diff := range diffs {
		// Convert nibbles key to bytes key
		diff.Key = hexToKeybytes(diff.Key)
	}

	return diffs, nil
}

func computeTrieDiff(left, right node, proofDB ethdb.KeyValueReader) ([]*Diff, error) {
	l, leftIsHash := left.(hashNode)
	r, rightIsHash := right.(hashNode)

	if leftIsHash && rightIsHash && bytes.Equal(l, r) {
		// both nodes are hash nodes and equal => no diff
		return nil, nil
	}

	// Resolve left and right nodes if they are hash nodes
	resolveNode := func(hash common.Hash) (node, error) {
		if hash == types.EmptyRootHash {
			return nil, nil
		}

		buf, _ := proofDB.Get(hash[:])
		if buf == nil {
			return nil, fmt.Errorf("proof node (hash %064x) missing", hash)
		}

		n, err := decodeNode(hash[:], buf)
		if err != nil {
			return nil, fmt.Errorf("bad proof node %v", err)
		}
		return n, nil
	}

	if leftIsHash {
		n, err := resolveNode(common.BytesToHash(l))
		if err != nil {
			return nil, err
		}
		return computeTrieDiff(n, right, proofDB)
	}

	if rightIsHash {
		n, err := resolveNode(common.BytesToHash(r))
		if err != nil {
			return nil, err
		}
		return computeTrieDiff(left, n, proofDB)
	}

	switch l := left.(type) {
	case nil:
		switch r := right.(type) {
		case nil:
			// both nodes are nil => no diff
			return nil, nil
		case valueNode:
			// left is nil, right is value => right value is a diff
			return []*Diff{{RightValue: r}}, nil
		case *shortNode:
			// left is nil, right is short => look for diffs in the right child
			diffs, err := computeTrieDiff(nil, r.Val, proofDB)
			if err != nil {
				return nil, err
			}
			return extendDiffKeys(r.Key, diffs), nil
		case *fullNode:
			// left is nil, right is full => look for diffs in all right's children
			diffs := make([]*Diff, 0)
			for i := range 16 {
				cldDiffs, err := computeTrieDiff(nil, r.Children[i], proofDB)
				if err != nil {
					return nil, err
				}
				diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
			}
			return diffs, nil

		}
	case valueNode:
		switch r := right.(type) {
		case nil:
			// left is value, right is nil => this is a diff
			return []*Diff{{LeftValue: l}}, nil
		case valueNode:
			// left is value, right is value => compare the values
			if bytes.Equal(l, r) {
				// both values are equal => no diff
				return nil, nil
			}
			// both values are different => this is a diff
			return []*Diff{{LeftValue: l, RightValue: r}}, nil
		case *shortNode:
			// left is value, right is short => everything is a diff
			// Note: this should never happen when comparing two valid trie roots
			diffs, err := computeTrieDiff(nil, r.Val, proofDB)
			if err != nil {
				return nil, err
			}
			diffs = append(extendDiffKeys(r.Key, diffs), &Diff{LeftValue: l})
			return diffs, nil
		case *fullNode:
			// left is value, right is full => left value is a diff + look for diffs in the right children
			// Note: this should never happen when comparing two valid trie roots
			diffs := []*Diff{{LeftValue: l}}
			for i := range 16 {
				cldDiffs, err := computeTrieDiff(nil, r.Children[i], proofDB)
				if err != nil {
					return nil, err
				}
				diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
			}
			return diffs, nil
		}
	case *shortNode:
		switch r := right.(type) {
		case nil:
			// left is short, right is nil => look for diffs in the left child
			diffs, err := computeTrieDiff(l.Val, nil, proofDB)
			if err != nil {
				return nil, err
			}
			return extendDiffKeys(l.Key, diffs), nil
		case *shortNode:
			// both nodes are short
			if bytes.Equal(l.Key, r.Key) {
				// if the keys are equal => find diffs in the children
				diffs, err := computeTrieDiff(l.Val, r.Val, proofDB)
				if err != nil {
					return nil, err
				}
				return extendDiffKeys(l.Key, diffs), nil
			}

			if bytes.HasPrefix(l.Key, r.Key) {
				// right is prefix of left
				// compare the right child with the left shortened by the right key
				// Note: the right child could be full and have common values with the left shortened
				diffs, err := computeTrieDiff(shortenBy(l, len(r.Key)), r.Val, proofDB)
				if err != nil {
					return nil, err
				}
				return extendDiffKeys(r.Key, diffs), nil
			}

			if bytes.HasPrefix(r.Key, l.Key) {
				// left is prefix of right
				// same as above
				diffs, err := computeTrieDiff(l.Val, shortenBy(r, len(l.Key)), proofDB)
				if err != nil {
					return nil, err
				}
				return extendDiffKeys(l.Key, diffs), nil
			}

			// left and right are different
			// look for diffs in the left child and the right child
			lDiffs, err := computeTrieDiff(l.Val, nil, proofDB)
			if err != nil {
				return nil, err
			}
			lDiffs = extendDiffKeys(l.Key, lDiffs)

			rDiffs, err := computeTrieDiff(nil, r.Val, proofDB)
			if err != nil {
				return nil, err
			}
			rDiffs = extendDiffKeys(r.Key, rDiffs)

			return append(lDiffs, rDiffs...), nil
		case *fullNode:
			// left is short, right is full
			diffs := make([]*Diff, 0)

			for i := range 16 {
				if l.Key[0] == branchIndices[i] {
					// left key matches the branch index => compare the left node shortened by the index with the right child
					cldDiffs, err := computeTrieDiff(shortenBy(l, 1), r.Children[i], proofDB)
					if err != nil {
						return nil, err
					}
					diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
				} else {
					// left key does not match the branch index => look for diffs in the right child
					cldDiffs, err := computeTrieDiff(nil, r.Children[i], proofDB)
					if err != nil {
						return nil, err
					}
					diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
				}
			}

			return diffs, nil
		}
	case *fullNode:
		switch r := right.(type) {
		case nil:
			// left is full, right is nil => look for diffs in the left children
			diffs := make([]*Diff, 0)
			for i := range 16 {
				cldDiffs, err := computeTrieDiff(l.Children[i], nil, proofDB)
				if err != nil {
					return nil, err
				}
				diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
			}
			return diffs, nil
		case *shortNode:
			// left is full, right is short
			diffs := make([]*Diff, 0)
			for i := range 16 {
				if r.Key[0] == branchIndices[i] {
					// right key matches the branch index => compare the left child with the right child shortened by 1
					cldDiffs, err := computeTrieDiff(l.Children[i], shortenBy(r, 1), proofDB)
					if err != nil {
						return nil, err
					}
					diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
				} else {
					// right key does not match the branch index => look for diffs in the left child
					cldDiffs, err := computeTrieDiff(l.Children[i], nil, proofDB)
					if err != nil {
						return nil, err
					}
					diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
				}
			}

			return diffs, nil
		case *fullNode:
			// both nodes are full
			diffs := make([]*Diff, 0)
			for i := range 16 {
				cldDiffs, err := computeTrieDiff(l.Children[i], r.Children[i], proofDB)
				if err != nil {
					return nil, err
				}
				diffs = append(diffs, extendDiffKeys([]byte{branchIndices[i]}, cldDiffs)...)
			}
			return diffs, nil
		}
	}

	return nil, nil
}

// shortenBy reduces the length of the key of a short node by the given length.
// If the key length is the given length, it returns the child node itself.
// If the key length is greater than the given length, it returns a new short node with the remaining key and the child node.
// If the key length is less than the given length, it panics.
func shortenBy(n *shortNode, length int) node {
	if len(n.Key) < length {
		panic(fmt.Sprintf("shortenBy: key is shorter than length: %v, %v", n.Key, length))
	}

	if len(n.Key) == length {
		return n.Val
	}

	return &shortNode{Key: n.Key[length:], Val: n.Val}
}

func extendDiffKeys(prefix []byte, diffs []*Diff) []*Diff {
	for _, d := range diffs {
		d.Key = append(prefix, d.Key...)
	}
	return diffs
}

// --- End fork code ---
