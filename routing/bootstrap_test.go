package routing

import (
	"context"
	"testing"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/require"

	"github.com/plprobelab/go-kademlia/internal/kadtest"
	"github.com/plprobelab/go-kademlia/kad"
	"github.com/plprobelab/go-kademlia/key"
	"github.com/plprobelab/go-kademlia/network/address"
	"github.com/plprobelab/go-kademlia/query"
	"github.com/plprobelab/go-kademlia/sim"
)

func TestBootstrapConfigValidate(t *testing.T) {
	t.Run("default is valid", func(t *testing.T) {
		cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
		require.NoError(t, cfg.Validate())
	})

	t.Run("clock is not nil", func(t *testing.T) {
		cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
		cfg.Clock = nil
		require.Error(t, cfg.Validate())
	})

	t.Run("timeout positive", func(t *testing.T) {
		cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
		cfg.Timeout = 0
		require.Error(t, cfg.Validate())
		cfg.Timeout = -1
		require.Error(t, cfg.Validate())
	})

	t.Run("request concurrency positive", func(t *testing.T) {
		cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
		cfg.RequestConcurrency = 0
		require.Error(t, cfg.Validate())
		cfg.RequestConcurrency = -1
		require.Error(t, cfg.Validate())
	})

	t.Run("request timeout positive", func(t *testing.T) {
		cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
		cfg.RequestTimeout = 0
		require.Error(t, cfg.Validate())
		cfg.RequestTimeout = -1
		require.Error(t, cfg.Validate())
	})

	t.Run("queue capacity positive", func(t *testing.T) {
		cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
		cfg.QueueCapacity = 0
		require.Error(t, cfg.Validate())
		cfg.QueueCapacity = -1
		require.Error(t, cfg.Validate())
	})
}

// simFindNodeRequest is a FindNodeRequestFunc that uses simulated messages
func simFindNodeRequest(n kad.NodeID[key.Key8]) (address.ProtocolID, kad.Request[key.Key8, kadtest.StrAddr]) {
	return address.ProtocolID("/sim/1.0.0"), sim.NewRequest[key.Key8, kadtest.StrAddr](n.Key())
}

func TestBootstrapStartsIdle(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewMock()
	cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
	cfg.Clock = clk

	self := kadtest.NewID(key.Key8(0))
	bs, err := NewBootstrap[key.Key8, kadtest.StrAddr](self, cfg)
	require.NoError(t, err)

	bs.Enqueue(ctx, &EventBootstrapPoll{})
	state := bs.Advance(ctx)
	require.IsType(t, &StateBootstrapIdle{}, state)
}

func TestBootstrapStart(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewMock()
	cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
	cfg.Clock = clk

	self := kadtest.NewID(key.Key8(0))
	bs, err := NewBootstrap[key.Key8, kadtest.StrAddr](self, cfg)
	require.NoError(t, err)

	a := kadtest.NewID(key.Key8(0b00000100)) // 4

	msg := kadtest.NewRequest("1", self.Key())
	protocolID := address.ProtocolID("testprotocol")

	// start the bootstrap
	bs.Enqueue(ctx, &EventBootstrapStart[key.Key8, kadtest.StrAddr]{
		ProtocolID:        protocolID,
		Message:           msg,
		KnownClosestNodes: []kad.NodeID[key.Key8]{a},
	})
	state := bs.Advance(ctx)
	require.IsType(t, &StateBootstrapMessage[key.Key8, kadtest.StrAddr]{}, state)

	// the query should attempt to contact the node it was given
	st := state.(*StateBootstrapMessage[key.Key8, kadtest.StrAddr])

	// the query should be the one just added
	require.Equal(t, query.QueryID("bootstrap"), st.QueryID)

	// the query should attempt to contact the node it was given
	require.Equal(t, a, st.NodeID)

	// with the correct protocol ID
	require.Equal(t, protocolID, st.ProtocolID)

	// with the correct message
	require.Equal(t, msg, st.Message)

	// now the bootstrap reports that it is waiting
	bs.Enqueue(ctx, &EventBootstrapPoll{})
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapWaiting{}, state)
}

func TestBootstrapMessageResponse(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewMock()
	cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
	cfg.Clock = clk

	self := kadtest.NewID(key.Key8(0))
	bs, err := NewBootstrap[key.Key8, kadtest.StrAddr](self, cfg)
	require.NoError(t, err)

	a := kadtest.NewID(key.Key8(0b00000100)) // 4

	msg := kadtest.NewRequest("1", self.Key())
	protocolID := address.ProtocolID("testprotocol")

	// start the bootstrap
	bs.Enqueue(ctx, &EventBootstrapStart[key.Key8, kadtest.StrAddr]{
		ProtocolID:        protocolID,
		Message:           msg,
		KnownClosestNodes: []kad.NodeID[key.Key8]{a},
	})
	state := bs.Advance(ctx)
	require.IsType(t, &StateBootstrapMessage[key.Key8, kadtest.StrAddr]{}, state)

	// the bootstrap should attempt to contact the node it was given
	st := state.(*StateBootstrapMessage[key.Key8, kadtest.StrAddr])
	require.Equal(t, query.QueryID("bootstrap"), st.QueryID)
	require.Equal(t, a, st.NodeID)

	// notify bootstrap that node was contacted successfully, but no closer nodes
	bs.Enqueue(ctx, &EventBootstrapMessageResponse[key.Key8, kadtest.StrAddr]{
		NodeID: a,
	})
	state = bs.Advance(ctx)

	// bootstrap should respond that its query has finished
	require.IsType(t, &StateBootstrapFinished{}, state)

	stf := state.(*StateBootstrapFinished)
	require.Equal(t, 1, stf.Stats.Requests)
	require.Equal(t, 1, stf.Stats.Success)
}

func TestBootstrapProgress(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewMock()
	cfg := DefaultBootstrapConfig[key.Key8, kadtest.StrAddr]()
	cfg.Clock = clk
	cfg.RequestConcurrency = 3 // 1 less than the 4 nodes to be visited

	self := kadtest.NewID(key.Key8(0))
	bs, err := NewBootstrap[key.Key8, kadtest.StrAddr](self, cfg)
	require.NoError(t, err)

	a := kadtest.NewID(key.Key8(0b00000100)) // 4
	b := kadtest.NewID(key.Key8(0b00001000)) // 8
	c := kadtest.NewID(key.Key8(0b00010000)) // 16
	d := kadtest.NewID(key.Key8(0b00100000)) // 32

	// ensure the order of the known nodes
	require.True(t, self.Key().Xor(a.Key()).Compare(self.Key().Xor(b.Key())) == -1)
	require.True(t, self.Key().Xor(b.Key()).Compare(self.Key().Xor(c.Key())) == -1)
	require.True(t, self.Key().Xor(c.Key()).Compare(self.Key().Xor(d.Key())) == -1)

	msg := kadtest.NewRequest("1", self.Key())
	protocolID := address.ProtocolID("testprotocol")

	// start the bootstrap
	bs.Enqueue(ctx, &EventBootstrapStart[key.Key8, kadtest.StrAddr]{
		ProtocolID:        protocolID,
		Message:           msg,
		KnownClosestNodes: []kad.NodeID[key.Key8]{d, a, b, c},
	})

	// the bootstrap should attempt to contact the closest node it was given
	state := bs.Advance(ctx)
	require.IsType(t, &StateBootstrapMessage[key.Key8, kadtest.StrAddr]{}, state)
	st := state.(*StateBootstrapMessage[key.Key8, kadtest.StrAddr])
	require.Equal(t, query.QueryID("bootstrap"), st.QueryID)
	require.Equal(t, a, st.NodeID)

	// next the bootstrap attempts to contact second nearest node
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapMessage[key.Key8, kadtest.StrAddr]{}, state)
	st = state.(*StateBootstrapMessage[key.Key8, kadtest.StrAddr])
	require.Equal(t, b, st.NodeID)

	// next the bootstrap attempts to contact third nearest node
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapMessage[key.Key8, kadtest.StrAddr]{}, state)
	st = state.(*StateBootstrapMessage[key.Key8, kadtest.StrAddr])
	require.Equal(t, c, st.NodeID)

	// now the bootstrap should be waiting since it is at request capacity
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapWaiting{}, state)

	// notify bootstrap that node was contacted successfully, but no closer nodes
	bs.Enqueue(ctx, &EventBootstrapMessageResponse[key.Key8, kadtest.StrAddr]{
		NodeID: a,
	})

	// now the bootstrap has capacity to contact fourth nearest node
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapMessage[key.Key8, kadtest.StrAddr]{}, state)
	st = state.(*StateBootstrapMessage[key.Key8, kadtest.StrAddr])
	require.Equal(t, d, st.NodeID)

	// notify bootstrap that all remaining nodes were contacted successfully
	bs.Enqueue(ctx, &EventBootstrapMessageResponse[key.Key8, kadtest.StrAddr]{
		NodeID: b,
	})

	// bootstrap should respond that it is waiting for messages
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapWaiting{}, state)

	// notify bootstrap that all remaining nodes were contacted successfully
	bs.Enqueue(ctx, &EventBootstrapMessageResponse[key.Key8, kadtest.StrAddr]{
		NodeID: c,
	})

	// bootstrap should respond that it is waiting for last message
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapWaiting{}, state)

	// notify bootstrap that all remaining nodes were contacted successfully
	bs.Enqueue(ctx, &EventBootstrapMessageResponse[key.Key8, kadtest.StrAddr]{
		NodeID: d,
	})

	// bootstrap should respond that its query has finished
	state = bs.Advance(ctx)
	require.IsType(t, &StateBootstrapFinished{}, state)

	stf := state.(*StateBootstrapFinished)
	require.Equal(t, 4, stf.Stats.Requests)
	require.Equal(t, 4, stf.Stats.Success)
}
