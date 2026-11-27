package layoutmgr

import (
	"testing"

	"github.com/asosick/iridium_layout_manager/views"
)

func TestCascadeFillsBreakpointsAsIridiumDoes(t *testing.T) {
	for _, tc := range []struct {
		in   map[string]int
		want breakpointColumns
	}{
		{nil, breakpointColumns{1, 1, 1, 1, 1, 1}},
		{map[string]int{"md": 3}, breakpointColumns{1, 1, 3, 3, 3, 3}},
		{map[string]int{"xs": 2, "lg": 4, "2xl": 6}, breakpointColumns{2, 2, 2, 4, 4, 6}},
		{map[string]int{"xs": 0, "sm": 99, "huge": 5}, breakpointColumns{1, 12, 12, 12, 12, 12}},
	} {
		if got := cascade(tc.in, maxGridColumns); got != tc.want {
			t.Errorf("cascade(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestWithWidestKeepsTheSmallerScreens(t *testing.T) {
	base := breakpointColumns{1, 1, 2, 4, 4, 4}
	for n, want := range map[int]breakpointColumns{
		6: {1, 1, 2, 6, 6, 6},
		4: base,
		3: {1, 1, 2, 3, 3, 3},
		1: {1, 1, 1, 1, 1, 1},
	} {
		if got := base.withWidest(n); got != want {
			t.Errorf("withWidest(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestGridColumnsBuilders(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	if got := p.baseColumns(); got != (breakpointColumns{1, 2, 2, 2, 2, 2}) {
		t.Fatalf("default columns %v", got)
	}
	if got := p.GridColumnsFixed(3).baseColumns(); got != (breakpointColumns{3, 3, 3, 3, 3, 3}) {
		t.Fatalf("GridColumnsFixed(3) gives %v", got)
	}
	columns := map[string]int{"xs": 1, "lg": 4}
	p.GridColumns(columns)
	columns["lg"] = 9
	if got := p.baseColumns(); got != (breakpointColumns{1, 1, 1, 4, 4, 4}) {
		t.Fatalf("GridColumns kept the caller's map: %v", got)
	}
	if got := p.maxColumns(); got != defaultMaxColumns {
		t.Fatalf("MaxColumns with a 4-column page is %d", got)
	}
	if got := p.GridColumns(map[string]int{"xl": 8}).maxColumns(); got != 8 {
		t.Fatalf("MaxColumns under the page's widest: %d", got)
	}
}

// The settings dialog's screen sizes are the breakpoints the server takes.
func TestViewsBreakpointsMatch(t *testing.T) {
	if len(views.Breakpoints) != len(breakpoints) {
		t.Fatalf("views has %d breakpoints, want %d", len(views.Breakpoints), len(breakpoints))
	}
	for i, bp := range views.Breakpoints {
		if bp.Key != breakpoints[i] {
			t.Errorf("views breakpoint %d is %q, want %q", i, bp.Key, breakpoints[i])
		}
	}
}
