package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listNodesAPIServer serves GET /api/v1/node returning the given nodes,
// mirroring the real Headscale list-nodes endpoint.
func listNodesAPIServer(t *testing.T, nodes []clientv1.Node) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		err := json.NewEncoder(w).Encode(clientv1.ListNodesOutputBody{Nodes: nodes})
		if err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
}

// newListNodeRoutesTestCmd builds a fresh cobra.Command wired to
// listNodeRoutesCmd.RunE with the identifier flag set and JSON output
// requested, so flag state does not leak between subtests. The CLI is pointed
// at serverURL via viper.
func newListNodeRoutesTestCmd(t *testing.T, serverURL, identifier string) *cobra.Command {
	t.Helper()

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("cli.address", serverURL)
	viper.Set("cli.api_key", "test-key")
	viper.Set("cli.timeout", "30s")

	cmd := &cobra.Command{
		Use:  listNodeRoutesCmd.Use,
		RunE: listNodeRoutesCmd.RunE,
	}
	cmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	cmd.Flags().StringP("output", "o", "", "Output format")

	if identifier != "" {
		require.NoError(t, cmd.Flags().Set("identifier", identifier))
	}

	require.NoError(t, cmd.Flags().Set("output", "json"))

	return cmd
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns the
// captured output. The original stdout is always restored.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	old := os.Stdout

	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w

	runErr := fn()

	w.Close()

	os.Stdout = old

	var buf bytes.Buffer

	_, readErr := buf.ReadFrom(r)
	require.NoError(t, readErr)

	return buf.String(), runErr
}

// TestListNodeRoutesCmdIdentifierNotFound is the regression test for the bug
// where `nodes list-routes --identifier <nonexistent>` silently printed every
// node's routes instead of erroring with "node not found".
func TestListNodeRoutesCmdIdentifierNotFound(t *testing.T) {
	nodeA := clientv1.Node{
		Id:             "1",
		GivenName:      "node-a",
		ApprovedRoutes: []string{"10.0.0.0/24"},
	}
	nodeB := clientv1.Node{
		Id:             "2",
		GivenName:      "node-b",
		ApprovedRoutes: []string{"10.1.0.0/24"},
	}

	server := listNodesAPIServer(t, []clientv1.Node{nodeA, nodeB})
	defer server.Close()

	// A nonexistent identifier must surface an error containing "node not
	// found", matching the server's 404 and every sibling command.
	cmd := newListNodeRoutesTestCmd(t, server.URL, "99999")
	_, err := captureStdout(t, func() error { return cmd.RunE(cmd, nil) })
	require.Error(t, err)
	assert.ErrorContains(t, err, "node not found")
}

// TestListNodeRoutesCmdIdentifierFound confirms that a valid identifier
// narrows the output to the requested node only (no regression in the happy
// path).
func TestListNodeRoutesCmdIdentifierFound(t *testing.T) {
	nodeA := clientv1.Node{
		Id:             "1",
		GivenName:      "node-a",
		ApprovedRoutes: []string{"10.0.0.0/24"},
	}
	nodeB := clientv1.Node{
		Id:             "2",
		GivenName:      "node-b",
		ApprovedRoutes: []string{"10.1.0.0/24"},
	}

	server := listNodesAPIServer(t, []clientv1.Node{nodeA, nodeB})
	defer server.Close()

	cmd := newListNodeRoutesTestCmd(t, server.URL, "1")
	out, err := captureStdout(t, func() error { return cmd.RunE(cmd, nil) })
	require.NoError(t, err)

	var got []clientv1.Node
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "1", got[0].Id)
	assert.Contains(t, got[0].ApprovedRoutes, "10.0.0.0/24")
}

// TestListNodeRoutesCmdNoIdentifier confirms that omitting --identifier
// still lists every route-bearing node (the default behaviour is unchanged).
func TestListNodeRoutesCmdNoIdentifier(t *testing.T) {
	nodeA := clientv1.Node{
		Id:             "1",
		GivenName:      "node-a",
		ApprovedRoutes: []string{"10.0.0.0/24"},
	}
	nodeB := clientv1.Node{
		Id:             "2",
		GivenName:      "node-b",
		ApprovedRoutes: []string{"10.1.0.0/24"},
	}
	// A node with no routes must be filtered out by the command.
	nodeNoRoutes := clientv1.Node{
		Id:        "3",
		GivenName: "node-c",
	}

	server := listNodesAPIServer(t, []clientv1.Node{nodeA, nodeB, nodeNoRoutes})
	defer server.Close()

	cmd := newListNodeRoutesTestCmd(t, server.URL, "")
	out, err := captureStdout(t, func() error { return cmd.RunE(cmd, nil) })
	require.NoError(t, err)

	var got []clientv1.Node
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Len(t, got, 2)
}

// TestListNodeRoutesCmdIdentifierNotFoundSingleNode confirms the bug is not
// masked when only a single route-bearing node exists: the nonexistent
// identifier must still error rather than returning that lone node's routes.
func TestListNodeRoutesCmdIdentifierNotFoundSingleNode(t *testing.T) {
	nodeA := clientv1.Node{
		Id:             "1",
		GivenName:      "node-a",
		ApprovedRoutes: []string{"10.0.0.0/24"},
	}

	server := listNodesAPIServer(t, []clientv1.Node{nodeA})
	defer server.Close()

	cmd := newListNodeRoutesTestCmd(t, server.URL, "99999")
	_, err := captureStdout(t, func() error { return cmd.RunE(cmd, nil) })
	require.Error(t, err)
	assert.ErrorContains(t, err, "node not found")
}
