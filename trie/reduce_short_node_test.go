// --- Start fork code ---
package trie

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortenShortNode(t *testing.T) {
	testCases := []struct {
		desc string
		sn   *shortNode

		expectedShortNodes []*ProofNode
		expectedErr        bool
	}{
		{
			desc: "one-nibble short node",
			sn: &shortNode{
				Key: []byte{0x01},
				Val: hashNode(types.EmptyRootHash[:]),
			},
			expectedShortNodes: []*ProofNode{},
		},
		{
			desc: "two-nibble short node",
			sn: &shortNode{
				Key: hexToCompact([]byte{0x02, 0x03}),
				Val: hashNode(types.EmptyRootHash[:]),
			},
			expectedShortNodes: []*ProofNode{
				{
					Node: trienode.New(
						common.HexToHash("0xee6d912465ac651ea64e262b2a6a7959985a2ed764b3f447f2c2579754d22b4d"),
						nodeToBytes(&shortNode{
							Key: hexToCompact([]byte{0x03}),
							Val: hashNode(types.EmptyRootHash[:]),
						})),
					Path: []byte{0x02},
				},
			},
		},
		{
			desc: "three-nibble short node",
			sn: &shortNode{
				Key: hexToCompact([]byte{0x02, 0x03, 0x04}),
				Val: hashNode(types.EmptyRootHash[:]),
			},
			expectedShortNodes: []*ProofNode{
				{
					Node: trienode.New(
						common.HexToHash("0x58272724a1d456c20850c109e4ba573b2abf62299c53b91aa1de037c581d901b"),
						nodeToBytes(&shortNode{
							Key: hexToCompact([]byte{0x03, 0x04}),
							Val: hashNode(types.EmptyRootHash[:]),
						})),
					Path: []byte{0x02},
				},
				{
					Node: trienode.New(
						common.HexToHash("0xcf82c31432c469d4cb55d2e2aa24e41fad1ecad128ccafd1e1463dadc04a5eac"),
						nodeToBytes(&shortNode{
							Key: hexToCompact([]byte{0x04}),
							Val: hashNode(types.EmptyRootHash[:]),
						})),
					Path: []byte{0x02, 0x03},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			shortNodes, err := ShortenShortNode(nodeToBytes(tc.sn))
			if tc.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedShortNodes, shortNodes)
			}
		})
	}
}

// --- End fork code ---
