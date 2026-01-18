package core

// Represents the basic structure of a value in consensus.
type Value interface {
	Hash() string
	IsNil() bool
	Equal(other Value) bool
}
