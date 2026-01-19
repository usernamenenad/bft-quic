package ibft_test

import (
	"context"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
)

func TestIbft(t *testing.T) {
	config := ibft.NewConfig(5, func(r core.Round) time.Duration {
		return 2 * time.Second
	})
	network := ibft.NewTransport()
	store := ibft.NewStore()

	i := ibft.NewIbft("", config, network, store, nil)

	value := ibft.NewValue([]byte("value-1"))

	i.Start(context.Background(), "instance-1", value)

	time.Sleep(10 * time.Second)
}
