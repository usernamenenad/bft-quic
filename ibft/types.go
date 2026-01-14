package ibft

import (
	"crypto/sha256"
	"encoding/hex"
)

type Round uint64

type ConsensusInstance string

type Value struct {
	Data []byte
}

func (v *Value) IsNil() bool {
	return v == nil || len(v.Data) == 0
}

func (v *Value) Hash() string {
	if v.IsNil() {
		return ""
	}

	h := sha256.Sum256(v.Data)
	return hex.EncodeToString(h[:])
}

func (v *Value) Equal(other *Value) bool {
	if v.IsNil() && other.IsNil() {
		return true
	}

	if v.IsNil() || other.IsNil() {
		return false
	}

	return v.Hash() == other.Hash()
}

type RoundChangeCert []*IbftMessage
