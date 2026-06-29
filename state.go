package layoutmgr

import "encoding/json"

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

// Layout is a single customizable page (a "layout" / "view" in the Filament
// original). Each layout holds its own ordered list of blocks. Users flip
// between layouts with the numbered selector buttons / cmd+N hotkeys.
type Layout struct {
	Blocks []Block `json:"blocks"`
}

// LayoutState is the full serialized state for one user. It holds one or more
// Layouts (pages). v1 stored a flat block list under "blocks"; that legacy
// shape is migrated transparently into Layouts[0] on load (see UnmarshalJSON).
type LayoutState struct {
	Layouts []Layout `json:"layouts"`
}

// UnmarshalJSON decodes the current ({"layouts":[{"blocks":[...]}]}) shape and
// transparently migrates the legacy single-layout shape ({"blocks":[...]}) into
// Layouts[0], so users who saved a layout before multi-page support keep it.
func (s *LayoutState) UnmarshalJSON(data []byte) error {
	var raw struct {
		Layouts []Layout `json:"layouts"`
		Blocks  []Block  `json:"blocks"` // legacy flat field
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Layouts = raw.Layouts
	if len(s.Layouts) == 0 && len(raw.Blocks) > 0 {
		s.Layouts = []Layout{{Blocks: raw.Blocks}}
	}
	return nil
}

// EnsureLayouts pads the Layouts slice so it has at least count entries. Used so
// a user can target any layout index in [0, count) even before they've added
// anything to it.
func (s *LayoutState) EnsureLayouts(count int) {
	if count < 1 {
		count = 1
	}
	for len(s.Layouts) < count {
		s.Layouts = append(s.Layouts, Layout{})
	}
}

// LayoutAt returns a pointer to the layout at index i (creating intermediate
// layouts as needed up to count). An out-of-range index clamps to 0.
func (s *LayoutState) LayoutAt(i, count int) *Layout {
	s.EnsureLayouts(count)
	if i < 0 || i >= len(s.Layouts) {
		i = 0
	}
	return &s.Layouts[i]
}

// ContentFlags returns a per-index bool slice of length count, true where the
// layout at that index has at least one block. Drives the "only show numbers
// for pages that have content" behaviour in view mode.
func (s *LayoutState) ContentFlags(count int) []bool {
	if count < 1 {
		count = 1
	}
	flags := make([]bool, count)
	for i := 0; i < count && i < len(s.Layouts); i++ {
		flags[i] = len(s.Layouts[i].Blocks) > 0
	}
	return flags
}

// UsedLayouts counts how many layouts (capped at count) hold at least one
// block. The selector strip stays hidden in view mode unless this is > 1.
func (s *LayoutState) UsedLayouts(count int) int {
	used := 0
	for _, has := range s.ContentFlags(count) {
		if has {
			used++
		}
	}
	return used
}

// FirstUsedLayout returns the index of the first layout that holds content, or
// 0 if every layout is empty. Used to focus a sensible default page on load.
func (s *LayoutState) FirstUsedLayout(count int) int {
	for i, has := range s.ContentFlags(count) {
		if has {
			return i
		}
	}
	return 0
}

// Find returns the block with the given ID and its index within this layout, or
// (nil, -1) if not present.
func (l *Layout) Find(id string) (*Block, int) {
	for i := range l.Blocks {
		if l.Blocks[i].ID == id {
			return &l.Blocks[i], i
		}
	}
	return nil, -1
}

// Remove drops the block with the given ID. Returns true if anything was
// removed.
func (l *Layout) Remove(id string) bool {
	_, i := l.Find(id)
	if i < 0 {
		return false
	}
	l.Blocks = append(l.Blocks[:i], l.Blocks[i+1:]...)
	return true
}

// Reorder reshuffles the blocks to match the given ID order. Unknown IDs are
// ignored; blocks present in the layout but missing from order are dropped to
// the end in their original relative order. Returns true if the order changed.
func (l *Layout) Reorder(order []string) bool {
	if len(order) == 0 {
		return false
	}
	byID := make(map[string]Block, len(l.Blocks))
	for _, b := range l.Blocks {
		byID[b.ID] = b
	}
	out := make([]Block, 0, len(l.Blocks))
	seen := make(map[string]bool, len(l.Blocks))
	for _, id := range order {
		if b, ok := byID[id]; ok && !seen[id] {
			out = append(out, b)
			seen[id] = true
		}
	}
	for _, b := range l.Blocks {
		if !seen[b.ID] {
			out = append(out, b)
		}
	}
	changed := len(out) != len(l.Blocks)
	if !changed {
		for i := range out {
			if out[i].ID != l.Blocks[i].ID {
				changed = true
				break
			}
		}
	}
	l.Blocks = out
	return changed
}

// Move shifts one block by delta positions while keeping every other block in
// its relative order. Out-of-range moves are clamped to the nearest edge.
func (l *Layout) Move(id string, delta int) bool {
	_, from := l.Find(id)
	if from < 0 || delta == 0 {
		return false
	}
	to := from + delta
	if to < 0 {
		to = 0
	}
	if to >= len(l.Blocks) {
		to = len(l.Blocks) - 1
	}
	if from == to {
		return false
	}

	block := l.Blocks[from]
	if from < to {
		copy(l.Blocks[from:to], l.Blocks[from+1:to+1])
	} else {
		copy(l.Blocks[to+1:from+1], l.Blocks[to:from])
	}
	l.Blocks[to] = block
	return true
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
