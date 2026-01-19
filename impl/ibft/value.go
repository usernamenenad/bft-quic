package ibft

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/usernamenenad/bft-quic/core"
)

type Value struct {
	Data []byte
}

func NewValue(data []byte) *Value {
	return &Value{
		Data: data,
	}
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

func (v *Value) Equal(other core.Value) bool {
	o, ok := other.(*Value)
	if !ok {
		return false
	}

	if v.IsNil() && o.IsNil() {
		return true
	}

	if v.IsNil() || o.IsNil() {
		return false
	}

	return v.Hash() == o.Hash()
}
