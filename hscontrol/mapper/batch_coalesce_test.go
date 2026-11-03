package mapper

import (
	"slices"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestProcessBatchedChangesCoalescesWhenInFlight verifies that at most one
// batched bundle per node is queued at a time. While a node's bundle is in
// flight, a new tick's changes must stay in pending (coalesced) rather than
// being queued as a second bundle that a non-FIFO worker could deliver out of
// order.
func TestProcessBatchedChangesCoalescesWhenInFlight(t *testing.T) {
	b := NewBatcher(50*time.Millisecond, 2, nil) // not started: no ticker, no workers

	id := types.NodeID(1)
	nc := newMultiChannelNodeConn(id, nil)
	nc.addConnection(makeConnectionEntry("conn-1", make(chan *tailcfg.MapResponse, 1)))
	b.nodes.Store(id, nc)

	// A bundle is in flight: the new change must be retained, not queued.
	nc.inFlight.Store(true)
	nc.appendPending(change.PolicyChange())
	b.processBatchedChanges()

	nc.pendingMu.Lock()
	retained := len(nc.pending)
	nc.pendingMu.Unlock()
	assert.Equal(t, 1, retained, "pending must be retained while a bundle is in flight")

	// No bundle in flight: the pending change is queued and the node marked
	// in-flight.
	nc.inFlight.Store(false)
	b.processBatchedChanges()

	nc.pendingMu.Lock()
	drained := len(nc.pending)
	nc.pendingMu.Unlock()
	assert.Equal(t, 0, drained, "pending must be drained once queued")
	assert.True(t, nc.inFlight.Load(), "queued bundle must mark the node in-flight")

	require.NotNil(t, b)
}

// TestProcessBatchedChangesSkipsNodesWithoutConnection drains a node that
// kept its batcher entry after its last stream closed. Rendering its changes
// is wasted, since send drops the result and its next stream opens with a
// full map, so no work may be queued for it.
func TestProcessBatchedChangesSkipsNodesWithoutConnection(t *testing.T) {
	b := NewBatcher(50*time.Millisecond, 2, nil) // not started: no ticker, no workers

	id := types.NodeID(1)
	nc := newMultiChannelNodeConn(id, nil)
	b.nodes.Store(id, nc)

	nc.appendPending(change.PolicyChange())
	b.processBatchedChanges()

	nc.pendingMu.Lock()
	pending := len(nc.pending)
	nc.pendingMu.Unlock()
	assert.Equal(t, 0, pending, "a node without a connection must not accumulate pending changes")
	assert.Empty(t, b.workCh, "no work may be queued for a node without a connection")
	assert.False(t, nc.inFlight.Load())
}

// TestAppendPendingStaysBoundedWhileInFlight appends a churn of joins,
// deletions and policy changes to a node whose bundle stays in flight, as
// when the workers fall behind. Below the cap, pending must stay as small as
// its coalesced form instead of growing by one entry per change, and
// draining it must render what coalescing the whole backlog at once would.
func TestAppendPendingStaysBoundedWhileInFlight(t *testing.T) {
	id := types.NodeID(1)
	nc := newMultiChannelNodeConn(id, nil)
	nc.inFlight.Store(true)

	var all []change.Change

	for i := range 3000 {
		peer := types.NodeID(i%50 + 2) //nolint:gosec // test

		cs := []change.Change{change.NodeAdded(peer), change.NodeOnline(peer), change.PolicyChange()}
		if i%100 == 0 {
			// The removal this node gets as a peer of the deleted one.
			cs = append(cs, change.NodeRemoved(types.NodeID(i+1000), id)[1]) //nolint:gosec // test
		}

		for _, c := range cs {
			nc.appendPending(c)
		}

		all = append(all, cs...)
	}

	nc.pendingMu.Lock()
	pending := slices.Clone(nc.pending)
	nc.pendingMu.Unlock()

	assert.Less(t, len(pending), 100, "pending must not grow with every change")
	assert.False(t, pending[0].IsFull(), "test sanity: the backlog stays below the cap")

	coalesce := func(cs []change.Change) []change.Change {
		return change.CoalescePeerDeltas(id, change.DedupePolicyChanges(cs))
	}
	assert.Equal(t, coalesce(all), coalesce(pending))
}

// TestAppendPendingBoundsFoldedPatches queues patch-only updates for one peer
// while a node's bundle stays in flight. Folding keeps each patch, so it must
// count them: a backlog folded into one change still grows by every patch,
// and only a collapse to a full map bounds it.
func TestAppendPendingBoundsFoldedPatches(t *testing.T) {
	id := types.NodeID(1)
	nc := newMultiChannelNodeConn(id, nil)
	nc.inFlight.Store(true)

	for i := range 10000 {
		if i%2 == 0 {
			nc.appendPending(change.NodeOnline(2))
		} else {
			nc.appendPending(change.NodeOffline(2))
		}
	}

	nc.pendingMu.Lock()
	pending := slices.Clone(nc.pending)
	nc.pendingMu.Unlock()

	require.NotEmpty(t, pending)
	assert.True(t, pending[0].IsFull(), "a backlog past the cap must collapse to a full map")
	assert.LessOrEqual(t, countPatches(pending), maxPendingSize, "pending must not keep every patch")
}

// TestAppendPendingFoldRecoversAfterOversizedFold pins the regression where a
// single fold whose output exceeded half the cap latched pendingFoldAt above
// maxPendingSize. Once latched, the fold condition (pendingSize >=
// pendingFoldAt) could never be satisfied before the collapse condition
// (pendingSize > maxPendingSize) fired, so every subsequent overflow collapsed
// to a full netmap instead of coalescing — degrading update efficiency for the
// life of the connection, which outlives reconnects. Clamping the re-armed
// threshold at the cap keeps the fold path reachable, so a backlog that
// re-fills after an oversized fold folds again instead of collapsing.
func TestAppendPendingFoldRecoversAfterOversizedFold(t *testing.T) {
	id := types.NodeID(1)
	nc := newMultiChannelNodeConn(id, nil)
	nc.inFlight.Store(true)

	// A single burst of patch-only changes for distinct peers coalesces into
	// one change carrying every patch; its post-fold size (1 + burstPeers)
	// exceeds half the cap, so 2*post-fold would arm the threshold above
	// maxPendingSize without the clamp.
	const burstPeers = 200

	burst := make([]change.Change, 0, burstPeers)
	for i := range burstPeers {
		burst = append(burst, change.NodeOnline(types.NodeID(i+1000)))
	}

	nc.appendPending(burst...)

	nc.pendingMu.Lock()
	require.NotEmpty(t, nc.pending)
	require.False(t, nc.pending[0].IsFull(), "burst must fold, not collapse")
	require.Greater(t, nc.pendingSize, maxPendingSize/2, "post-fold size must exceed half the cap to arm the latch")
	assert.LessOrEqual(t, nc.pendingFoldAt, maxPendingSize, "pendingFoldAt must never arm above the collapse cap")
	nc.pendingMu.Unlock()

	// A drain models the in-flight bundle being delivered; pending clears but
	// the latched pendingFoldAt is the state that survives it.
	nc.drainPending()

	// Re-filling the backlog under the same connection must fold again, not
	// collapse. Append patch-only changes for distinct peers one at a time:
	// each contributes 2 size units, so the fold (fixed) or collapse (buggy)
	// fires around maxPendingSize/2 appends.
	const trailingPeers = maxPendingSize/2 + 5

	for i := range trailingPeers {
		nc.appendPending(change.NodeOnline(types.NodeID(i + 2000)))
	}

	nc.pendingMu.Lock()
	pending := slices.Clone(nc.pending)
	nc.pendingMu.Unlock()

	require.NotEmpty(t, pending)
	assert.False(t, pending[0].IsFull(), "a re-filled backlog must fold, not collapse to a full netmap, after an oversized fold")
	assert.Positive(t, countPatches(pending), "the re-filled backlog must be coalesced into patches, not collapsed to a full map")

	// Folding must keep working across drain cycles, not just once. Drain
	// and re-fill a second time and require the same behavior: a re-connect
	// inherits the connection object, so a stale threshold must not disable
	// folding on the next backlog either.
	nc.drainPending()

	for i := range trailingPeers {
		nc.appendPending(change.NodeOnline(types.NodeID(i + 3000)))
	}

	nc.pendingMu.Lock()
	pending = slices.Clone(nc.pending)
	nc.pendingMu.Unlock()

	assert.False(t, pending[0].IsFull(), "folding must remain reachable across drain cycles")
	assert.Positive(t, countPatches(pending), "the backlog must remain coalesced across drain cycles")
}
