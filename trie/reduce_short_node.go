// --- Start fork code ---
package trie

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

// ShortenShortNode returns all possible short nodes that have the same child value as the original node but a shorter prefix key
// n is the proof node to shorten
func ShortenShortNode(n []byte) ([]*ProofNode, error) {
	decodedNode, err := decodeNode(crypto.Keccak256(n), n)
	if err != nil {
		return nil, fmt.Errorf("bad proof node %v", err)
	}
	// The post-state proof is a valid exclusion proof
	// We now assess if the deletion of the key resulted in a trie reduction by checking the last proof node (post-state)
	sn, ok := decodedNode.(*shortNode)
	if !ok {
		// The last proof node in the post-state is not a short node, this means that the deletion did not
		// result in any trie reduction, so there is no need to add orphan nodes to the pre-state trie
		return nil, fmt.Errorf("node %x is not a short node", n)
	}

	shortNodes := make([]*ProofNode, 0)

	// If the node is a one-nibbleshort node, it can not be reduced further
	if len(sn.Key) <= 1 {
		return shortNodes, nil
	}

	hasher := newHasher(false)
	defer returnHasherToPool(hasher)

	// Loop through all possible prefixes of the original node key
	for i := 1; i < len(sn.Key); i++ {
		collapsed, hashed := hasher.proofHash(&shortNode{
			Key: sn.Key[i:],
			Val: sn.Val,
		})

		if hash, ok := hashed.(hashNode); ok {
			// If the node's database encoding is a hash (or is the
			// root node), it becomes a proof element.
			shortNodes = append(shortNodes, &ProofNode{
				Node: trienode.New(common.BytesToHash(hash[:]), nodeToBytes(collapsed)),
				Path: sn.Key[:i],
			})
		}
	}

	return shortNodes, nil
}

// --- End fork code ---
