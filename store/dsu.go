package store

import (
	"sync"
)

// ContactDSU implements the Disjoint-set data structure for identity resolution.
// Why: Enables O(α(N)) amortized time complexity for finding canonical identities and merging contacts.
type ContactDSU struct {
	mu     sync.RWMutex
	parent map[int64]int64
	rank   map[int64]int
}

// NewContactDSU initializes a new DSU for contact management.
func NewContactDSU() *ContactDSU {
	return &ContactDSU{
		parent: make(map[int64]int64),
		rank:   make(map[int64]int),
	}
}

// Reset clears the DSU state.
func (d *ContactDSU) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.parent = make(map[int64]int64)
	d.rank = make(map[int64]int)
}

// Find returns the canonical ID (root) of the set containing id.
// Why: Uses path compression to flatten the structure during traversal for maximum efficiency.
func (d *ContactDSU) Find(id int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.findInternal(id)
}

func (d *ContactDSU) findInternal(id int64) int64 {
	p, exists := d.parent[id]
	if !exists || p == id {
		d.parent[id] = id
		return id
	}

	// Recursive call with path compression
	root := d.findInternal(p)
	d.parent[id] = root
	return root
}

// Union merges two disjoint sets represented by idA and idB.
// Why: Uses union-by-rank to maintain balanced trees, preventing degenerate O(N) chains.
func (d *ContactDSU) Union(idA, idB int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	rootA := d.findInternal(idA)
	rootB := d.findInternal(idB)
	if rootA == rootB {
		return rootA
	}

	return d.linkRoots(rootA, rootB)
}

func (d *ContactDSU) linkRoots(rootA, rootB int64) int64 {
	if d.rank[rootA] < d.rank[rootB] {
		d.parent[rootA] = rootB
		return rootB
	}

	if d.rank[rootA] == d.rank[rootB] {
		d.rank[rootA]++
	}

	d.parent[rootB] = rootA
	return rootA
}
