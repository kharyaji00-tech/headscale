package v2

import (
	"net/netip"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

var (
	exitRouteV4 = netip.MustParsePrefix("0.0.0.0/0")
	exitRouteV6 = netip.MustParsePrefix("::/0")
)

// exitNode builds a NodeView that advertises and has approved exit
// routes, so [types.NodeView.IsExitNode] reports true.
func exitNode() types.NodeView {
	routes := []netip.Prefix{exitRouteV4, exitRouteV6}

	return (&types.Node{
		ID:        1,
		GivenName: "exit",
		Hostinfo: &tailcfg.Hostinfo{
			RoutableIPs: routes,
		},
		ApprovedRoutes: routes,
	}).View()
}

// plainNode builds a NodeView with no exit routes.
func plainNode() types.NodeView {
	return (&types.Node{
		ID:        2,
		GivenName: "plain",
		Hostinfo:  &tailcfg.Hostinfo{},
	}).View()
}

func TestPeerCapMapSuggestExitNodeOnExitNode(t *testing.T) {
	t.Parallel()

	peer := exitNode()
	selfCaps := tailcfg.NodeCapMap{}

	got := PeerCapMap(peer, selfCaps)
	assert.Contains(t, got, nodecap.SuggestExitNode,
		"suggest-exit-node must be emitted for an approved exit node even without a grant")
	assert.NotContains(t, got, nodecap.DNSSubdomainResolve)
}

func TestPeerCapMapSuggestExitNodeAbsentOnNonExit(t *testing.T) {
	t.Parallel()

	peer := plainNode()
	selfCaps := tailcfg.NodeCapMap{}

	got := PeerCapMap(peer, selfCaps)
	assert.Nil(t, got, "non-exit node with no granted peer-consumed cap must yield nil")
}

func TestPeerCapMapDNSSubdomainResolveOnNonExit(t *testing.T) {
	t.Parallel()

	// The core bug: dns-subdomain-resolve is grant-gated, not
	// exit-gated, so it must appear on the peer view of a non-exit
	// node when the grant is present.
	peer := plainNode()
	selfCaps := tailcfg.NodeCapMap{
		nodecap.DNSSubdomainResolve: nil,
	}

	got := PeerCapMap(peer, selfCaps)
	if assert.NotNil(t, got) {
		assert.Contains(t, got, nodecap.DNSSubdomainResolve,
			"dns-subdomain-resolve must be emitted on the peer view when granted")
		assert.NotContains(t, got, nodecap.SuggestExitNode,
			"suggest-exit-node must not appear for a non-exit node")
	}
}

func TestPeerCapMapDNSSubdomainResolveAbsentWithoutGrant(t *testing.T) {
	t.Parallel()

	peer := plainNode()
	selfCaps := tailcfg.NodeCapMap{}

	got := PeerCapMap(peer, selfCaps)
	assert.Nil(t, got, "non-exit node without a dns-subdomain-resolve grant must yield nil")
}

func TestPeerCapMapBothCapsOnExitNodeWithGrant(t *testing.T) {
	t.Parallel()

	// An exit node that also has a dns-subdomain-resolve grant must
	// carry both caps on its peer view.
	peer := exitNode()
	selfCaps := tailcfg.NodeCapMap{
		nodecap.DNSSubdomainResolve: nil,
	}

	got := PeerCapMap(peer, selfCaps)
	assert.Contains(t, got, nodecap.SuggestExitNode)
	assert.Contains(t, got, nodecap.DNSSubdomainResolve)
}

func TestPeerCapMapSuggestExitNodeWithPolicyValue(t *testing.T) {
	t.Parallel()

	// A policy-granted suggest-exit-node value (non-nil) must be
	// preserved on the peer view; the emission condition is
	// IsExitNode, the value comes from peerSelfCaps.
	peer := exitNode()
	val := []tailcfg.RawMessage{tailcfg.RawMessage(`"value"`)}
	selfCaps := tailcfg.NodeCapMap{
		nodecap.SuggestExitNode: val,
	}

	got := PeerCapMap(peer, selfCaps)
	assert.Contains(t, got, nodecap.SuggestExitNode)
	assert.Equal(t, val, got[nodecap.SuggestExitNode])
}

func TestPeerCapMapDNSSubdomainResolveWithPolicyValue(t *testing.T) {
	t.Parallel()

	// A policy-granted dns-subdomain-resolve value (non-nil) must be
	// preserved on the peer view.
	peer := plainNode()
	val := []tailcfg.RawMessage{tailcfg.RawMessage(`"value"`)}
	selfCaps := tailcfg.NodeCapMap{
		nodecap.DNSSubdomainResolve: val,
	}

	got := PeerCapMap(peer, selfCaps)
	if assert.NotNil(t, got) {
		assert.Contains(t, got, nodecap.DNSSubdomainResolve)
		assert.Equal(t, val, got[nodecap.DNSSubdomainResolve])
	}
}

func TestPeerCapMapNilSelfCaps(t *testing.T) {
	t.Parallel()

	// A nil peerSelfCaps must not panic and must yield nil for a
	// non-exit node, and a non-nil map with only SuggestExitNode for
	// an exit node.
	got := PeerCapMap(plainNode(), nil)
	assert.Nil(t, got)

	got = PeerCapMap(exitNode(), nil)
	if assert.NotNil(t, got) {
		assert.Contains(t, got, nodecap.SuggestExitNode)
	}
}
