package handlers

import (
	"testing"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
)

func testNodes(n int) []*magnetarapi.NodeStringified {
	nodes := make([]*magnetarapi.NodeStringified, n)
	for i := 0; i < n; i++ {
		nodes[i] = &magnetarapi.NodeStringified{Id: "node-" + string(rune('a'+i))}
	}
	return nodes
}

func TestSelectRandmNodesEmpty(t *testing.T) {
	if got := selectRandmNodes(nil, 50); len(got) != 0 {
		t.Fatalf("expected no nodes, got %d", len(got))
	}
	if got := selectRandmNodes([]*magnetarapi.NodeStringified{}, 50); len(got) != 0 {
		t.Fatalf("expected no nodes, got %d", len(got))
	}
}

func TestSelectRandmNodesZeroAndNegativePercentage(t *testing.T) {
	for _, pct := range []int32{0, -1, -100} {
		if got := selectRandmNodes(testNodes(3), pct); len(got) != 0 {
			t.Fatalf("percentage %d: expected no nodes, got %d", pct, len(got))
		}
	}
}

func TestSelectRandmNodesFullPercentage(t *testing.T) {
	nodes := testNodes(4)
	got := selectRandmNodes(nodes, 100)
	if len(got) != len(nodes) {
		t.Fatalf("expected %d nodes, got %d", len(nodes), len(got))
	}
}

func TestSelectRandmNodesOverHundredPercentClamps(t *testing.T) {
	nodes := testNodes(3)
	got := selectRandmNodes(nodes, 150)
	if len(got) != len(nodes) {
		t.Fatalf("expected %d nodes, got %d", len(nodes), len(got))
	}
}

func TestSelectRandmNodesHalf(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want int
	}{
		{1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 3}, {10, 5},
	} {
		got := selectRandmNodes(testNodes(tc.n), 50)
		if len(got) != tc.want {
			t.Fatalf("n=%d: expected %d nodes, got %d", tc.n, tc.want, len(got))
		}
	}
}

func TestSelectRandmNodesDoesNotMutateInput(t *testing.T) {
	nodes := testNodes(5)
	before := make([]string, len(nodes))
	for i, n := range nodes {
		before[i] = n.Id
	}

	_ = selectRandmNodes(nodes, 50)

	if len(nodes) != len(before) {
		t.Fatalf("input slice length changed: %d -> %d", len(before), len(nodes))
	}
	for i, n := range nodes {
		if n.Id != before[i] {
			t.Fatalf("input slice mutated at %d: %s -> %s", i, before[i], n.Id)
		}
	}
}

func TestSelectRandmNodesReturnsSubsetWithoutDuplicates(t *testing.T) {
	nodes := testNodes(6)
	selected := selectRandmNodes(nodes, 50)

	seen := make(map[string]bool)
	for _, n := range selected {
		if seen[n.Id] {
			t.Fatalf("duplicate node %s", n.Id)
		}
		seen[n.Id] = true
	}
	all := make(map[string]bool)
	for _, n := range nodes {
		all[n.Id] = true
	}
	for _, n := range selected {
		if !all[n.Id] {
			t.Fatalf("selected node %s not in input", n.Id)
		}
	}
}
