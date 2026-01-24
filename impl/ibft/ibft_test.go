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
		return 50 * time.Second
	})
	network := ibft.NewTransport(5)
	storeLeader := ibft.NewStore()
	storeNonLeader2 := ibft.NewStore()
	storeNonLeader3 := ibft.NewStore()
	storeNonLeader4 := ibft.NewStore()
	storeNonLeader5 := ibft.NewStore()

	i1 := ibft.NewIbft("leader1", config, network, storeLeader, nil)
	i2 := ibft.NewIbft("non-leader2", config, network, storeNonLeader2, nil)
	i3 := ibft.NewIbft("non-leader3", config, network, storeNonLeader3, nil)
	i4 := ibft.NewIbft("non-leader4", config, network, storeNonLeader4, nil)
	i5 := ibft.NewIbft("non-leader5", config, network, storeNonLeader5, nil)

	value := ibft.NewValue([]byte("value-1"))

	go i1.Start(context.Background(), "instance-1", value)
	go i2.Start(context.Background(), "instance-1", value)
	go i3.Start(context.Background(), "instance-1", value)
	go i4.Start(context.Background(), "instance-1", value)
	go i5.Start(context.Background(), "instance-1", value)

	time.Sleep(50 * time.Second)
}
