package layoutmgr

import "maps"

// breakpoints are the screen sizes a column count can be given for, smallest
// first: the names Iridium's own column maps take (SchemaColumns,
// HeaderColumns), at the same viewport widths (xs any width, sm 40rem, md
// 48rem, lg 64rem, xl 80rem, 2xl 96rem; see layout_manager.css). Each applies
// from its width up, until a larger breakpoint sets another.
var breakpoints = [6]string{"xs", "sm", "md", "lg", "xl", "2xl"}

// breakpointColumns is a column count for each of breakpoints, in order.
type breakpointColumns [6]int

// defaultGridColumns is the grid a page has without GridColumns: one column
// on phones, two from the sm breakpoint up.
var defaultGridColumns = map[string]int{"xs": 1, "sm": 2}

// cascade fills in every breakpoint of a column map as Iridium does: a
// breakpoint left out takes the count of the next smaller one, and xs
// defaults to 1. Keys that are not breakpoints are ignored, and every count
// is kept to [1, most].
func cascade(columns map[string]int, most int) breakpointColumns {
	var out breakpointColumns
	last := 1
	for i, bp := range breakpoints {
		if n, ok := columns[bp]; ok {
			last = n
		}
		out[i] = clampCols(last, most)
	}
	return out
}

// widest is the most columns the page has at any breakpoint. Block spans are
// stored against it: a block as wide as the page at its widest is full width
// at every breakpoint.
func (c breakpointColumns) widest() int {
	n := 1
	for _, v := range c {
		n = max(n, v)
	}
	return n
}

// toMap is c as a column map with every breakpoint set.
func (c breakpointColumns) toMap() map[string]int {
	out := make(map[string]int, len(breakpoints))
	for i, bp := range breakpoints {
		out[bp] = c[i]
	}
	return out
}

// withWidest is c with its widest count changed to n: the breakpoints at the
// widest take n, and the others keep their count, up to n. This is what
// choosing a number of columns in the settings dialog does, so a page that
// gets more columns on a desktop keeps its phone layout.
func (c breakpointColumns) withWidest(n int) breakpointColumns {
	top := c.widest()
	for i, v := range c {
		if v == top {
			c[i] = n
		} else {
			c[i] = min(v, n)
		}
	}
	return c
}

// cloneColumns copies a column map, so a builder's caller can keep changing
// the map it passed without changing the page.
func cloneColumns(columns map[string]int) map[string]int {
	if columns == nil {
		return nil
	}
	return maps.Clone(columns)
}
