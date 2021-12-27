package types

import (
	"math/big"
	"github.com/ethereum/go-ethereum/common"
)

//try to assemble a block from a received header and body, return nil of no
//match
func BlockAssemble(h *Header, body *Body) *Block {
	b := &Block{ header: 	h,
				 transactions: body.Transactions,
				 uncles:	body.Uncles,
				 td: 		new(big.Int)}
	var hash common.Hash
	if len(body.Transactions) == 0 {
		hash = EmptyRootHash
	} else {
		hash = DeriveSha(Transactions(body.Transactions))
	}
	if hash != h.TxHash {
		return nil
	}
	
	if len(body.Uncles) == 0 {
		hash = EmptyUncleHash
	} else {
		hash = CalcUncleHash(body.Uncles)
	}
	if hash != h.UncleHash {
		return nil
	}
	return b
}
