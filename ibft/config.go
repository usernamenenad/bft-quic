package ibft

type Config struct {
	N uint64
	F uint64
}

func (c *Config) QuorumSize() uint64 {
	return (c.N+c.F)/2 + 1
}
