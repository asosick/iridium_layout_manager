package layoutmgr

// Block is one placed instance inside the user's layout.
//
//   - ID is a stable per-instance identifier (UUID) so the front-end can target
//     a specific block for resize/remove without confusing it with siblings of
//     the same Type.
//   - Type matches a BlockSpec.Key() — the LayoutManagerPage looks up the spec
//     by Type to render the block.
//   - Cols is the column span this instance takes inside the layout grid,
//     clamped to [1, GridColumns].
type Block struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Cols int    `json:"cols"`
}

// LayoutState is the full serialized state for one user's layout. v1 keeps a
// flat list of blocks; multi-layout support would add a slice of these (one
// per layout tab) — left out of v1 for simplicity per spec.
type LayoutState struct {
	Blocks []Block `json:"blocks"`
}

// Find returns the block with the given ID and its index, or (nil, -1) if not
// present.
func (s *LayoutState) Find(id string) (*Block, int) {
	for i := range s.Blocks {
		if s.Blocks[i].ID == id {
			return &s.Blocks[i], i
		}
	}
	return nil, -1
}

// Remove drops the block with the given ID. Returns true if anything was
// removed.
func (s *LayoutState) Remove(id string) bool {
	_, i := s.Find(id)
	if i < 0 {
		return false
	}
	s.Blocks = append(s.Blocks[:i], s.Blocks[i+1:]...)
	return true
}

// Reorder reshuffles the blocks to match the given ID order. Unknown IDs are
// ignored; blocks present in the state but missing from order are dropped to
// the end in their original relative order. Returns true if the order changed.
func (s *LayoutState) Reorder(order []string) bool {
	if len(order) == 0 {
		return false
	}
	byID := make(map[string]Block, len(s.Blocks))
	for _, b := range s.Blocks {
		byID[b.ID] = b
	}
	out := make([]Block, 0, len(s.Blocks))
	seen := make(map[string]bool, len(s.Blocks))
	for _, id := range order {
		if b, ok := byID[id]; ok && !seen[id] {
			out = append(out, b)
			seen[id] = true
		}
	}
	for _, b := range s.Blocks {
		if !seen[b.ID] {
			out = append(out, b)
		}
	}
	changed := len(out) != len(s.Blocks)
	if !changed {
		for i := range out {
			if out[i].ID != s.Blocks[i].ID {
				changed = true
				break
			}
		}
	}
	s.Blocks = out
	return changed
}

// clampCols snaps n to [1, max], using max as the upper bound. Used after a
// resize event and when re-rendering with a possibly-reduced GridColumns.
func clampCols(n, max int) int {
	if max < 1 {
		max = 1
	}
	if n < 1 {
		return 1
	}
	if n > max {
		return max
	}
	return n
}
