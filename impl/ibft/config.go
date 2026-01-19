package ibft

import (
	"time"

	"github.com/usernamenenad/bft-quic/core"
)

type Config struct {
	N       uint64
	Timeout func(round core.Round) time.Duration
}

func NewConfig(n uint64, timeout func(core.Round) time.Duration) *Config {
	return &Config{
		N:       n,
		Timeout: timeout,
	}
}

func (c *Config) F() uint64 {
	return (c.N - 1) / 3
}

func (c *Config) QuorumSize() uint64 {
	return 2*c.F() + 1
}
