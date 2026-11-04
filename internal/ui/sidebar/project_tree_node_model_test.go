package sidebar

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// The per-node loading lifecycle is a mirror machine (verification-only, in
// the style of internal/git's cleanup-phase model): production keeps the
// pendingRequest token and loadErr marker as fields, and this table
// documents — and checks — the protocol every node follows. pendingRequest
// is both the loading marker and the staleness token a result must still
// match, so "pending" is the only state a result may apply to.
type treeNodeState uint8

const (
	nodeUnloaded treeNodeState = iota // no read in flight, no error marker
	nodePending                       // a read is queued or executing under a request token
	nodeLoaded                        // children installed from a listing
	nodeFailed                        // last read failed; loadErr carries the marker
)

func (s treeNodeState) String() string {
	switch s {
	case nodePending:
		return "pending"
	case nodeLoaded:
		return "loaded"
	case nodeFailed:
		return "failed"
	default:
		return "unloaded"
	}
}

type treeNodeEvent uint8

const (
	evNodeRequest   treeNodeEvent = iota // enqueueLoad stamped a request token
	evNodeApplied                        // the matching result installed children
	evNodeFailed                         // the matching result errored
	evNodeDiscarded                      // collapse/invalidate dropped the request
	evNodeCollapse                       // collapseNode hid the subtree
)

func (e treeNodeEvent) String() string {
	switch e {
	case evNodeRequest:
		return "request"
	case evNodeApplied:
		return "applied"
	case evNodeFailed:
		return "failed"
	case evNodeDiscarded:
		return "discarded"
	default:
		return "collapse"
	}
}

var treeNodeMachine = fsm.MustCompile(fsm.Machine[treeNodeState, treeNodeEvent]{
	Name:    "project-tree-node",
	Initial: nodeUnloaded,
	Steps: []fsm.Step[treeNodeState, treeNodeEvent]{
		{From: nodeUnloaded, Event: evNodeRequest, To: nodePending},
		// A re-expansion re-reads even a loaded directory (refresh on
		// expand), and a failed node's retry clears the marker.
		{From: nodeLoaded, Event: evNodeRequest, To: nodePending},
		{From: nodeFailed, Event: evNodeRequest, To: nodePending},

		{From: nodePending, Event: evNodeApplied, To: nodeLoaded},
		{From: nodePending, Event: evNodeFailed, To: nodeFailed},
		// The node was collapsed (or the whole tree invalidated) while its
		// read ran: the token clears and nothing installs.
		{From: nodePending, Event: evNodeDiscarded, To: nodeUnloaded},

		// Collapse keeps installed children (only visibility changes) but
		// clears a failure marker and any pending token.
		{From: nodeLoaded, Event: evNodeCollapse, To: nodeLoaded},
		{From: nodeFailed, Event: evNodeCollapse, To: nodeUnloaded},
		{From: nodeUnloaded, Event: evNodeCollapse, To: nodeUnloaded},
	},
})

// TestTreeNodeMachineFormallyChecked verifies the node loading protocol:
// results apply only from pending (the request-token identity check), a
// request never stacks over an in-flight one (enqueueLoad's dedupe), and
// every state can still reach loaded (a failed or collapsed directory can
// always be expanded into a successful read).
func TestTreeNodeMachineFormallyChecked(t *testing.T) {
	if err := treeNodeMachine.Check(fsm.Checker[treeNodeState, treeNodeEvent]{
		Alphabet:             []treeNodeEvent{evNodeRequest, evNodeApplied, evNodeFailed, evNodeDiscarded, evNodeCollapse},
		AllDeclaredReachable: true,
		Edge: func(from treeNodeState, event treeNodeEvent, to treeNodeState) error {
			switch event {
			case evNodeApplied, evNodeFailed:
				if from != nodePending {
					return errors.New("a result applies only to a node with a matching in-flight request")
				}
			case evNodeRequest:
				if from == nodePending {
					return errors.New("a request over an in-flight request is the duplicate enqueueLoad dedupes")
				}
			case evNodeCollapse:
				if to == nodePending || to == nodeFailed {
					return errors.New("collapsing must not leave a pending read or a failure marker")
				}
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("tree node machine failed model checking: %v", err)
	}
	for _, st := range []treeNodeState{nodeUnloaded, nodePending, nodeLoaded, nodeFailed} {
		reached := false
		for _, next := range treeNodeMachine.ReachableFrom(st, nil) {
			if next == nodeLoaded {
				reached = true
			}
		}
		if !reached {
			t.Errorf("state %v cannot reach loaded: a directory must always be expandable into content", st)
		}
	}
}
