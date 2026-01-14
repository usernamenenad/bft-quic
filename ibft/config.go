package ibft

type Config struct {
	N uint64
}

func (c *Config) F() uint64 {
	return (c.N - 1) / 3
}

func (c *Config) QuorumSize() uint64 {
	return 2*c.F() + 1
}
