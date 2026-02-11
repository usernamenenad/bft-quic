package ibft

import (
	"time"

	"github.com/usernamenenad/bft-quic/core"
)

type Config struct {
	N          uint64
	Validators []core.NodeId
	Timeout    func(round core.Round) time.Duration
}

func NewConfig(validators []core.NodeId, timeout func(core.Round) time.Duration) *Config {
	return &Config{
		N:          uint64(len(validators)),
		Validators: validators,
		Timeout:    timeout,
	}
}

func (c *Config) F() uint64 {
	return (c.N - 1) / 3
}

func (c *Config) QuorumSize() uint64 {
	return 2*c.F() + 1
}
