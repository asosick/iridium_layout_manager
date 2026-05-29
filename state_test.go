package layoutmgr

import (
	"encoding/json"
	"testing"
)

func TestLayout_FindAndRemove(t *testing.T) {
	l := Layout{Blocks: []Block{
		{ID: "a", Type: "stat", Cols: 1},
		{ID: "b", Type: "chart", Cols: 2},
		{ID: "c", Type: "stat", Cols: 1},
	}}

	b, i := l.Find("b")
	if i != 1 || b == nil || b.Type != "chart" {
		t.Fatalf("Find(b): expected index 1 / chart, got i=%d b=%v", i, b)
	}

	if got, _ := l.Find("zzz"); got != nil {
		t.Errorf("Find for missing id should be nil, got %v", got)
	}

	if !l.Remove("b") {
		t.Fatalf("Remove(b) returned false")
	}
	if len(l.Blocks) != 2 || l.Blocks[0].ID != "a" || l.Blocks[1].ID != "c" {
		t.Fatalf("after Remove(b), expected [a c], got %v", l.Blocks)
	}

	if l.Remove("zzz") {
		t.Errorf("Remove(missing) should return false")
	}
}

func TestLayout_Reorder(t *testing.T) {
	t.Run("full reorder", func(t *testing.T) {
		l := Layout{Blocks: []Block{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
		if !l.Reorder([]string{"c", "a", "b"}) {
			t.Fatalf("expected reorder to report changed=true")
		}
		got := []string{l.Blocks[0].ID, l.Blocks[1].ID, l.Blocks[2].ID}
		if got[0] != "c" || got[1] != "a" || got[2] != "b" {
			t.Fatalf("expected [c a b], got %v", got)
		}
	})

	t.Run("partial order appends missing", func(t *testing.T) {
		l := Layout{Blocks: []Block{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
		l.Reorder([]string{"c"})
		got := []string{l.Blocks[0].ID, l.Blocks[1].ID, l.Blocks[2].ID}
		if got[0] != "c" || got[1] != "a" || got[2] != "b" {
			t.Fatalf("expected [c a b], got %v", got)
		}
	})

	t.Run("unknown ids ignored", func(t *testing.T) {
		l := Layout{Blocks: []Block{{ID: "a"}, {ID: "b"}}}
		l.Reorder([]string{"zzz", "b", "yyy"})
		got := []string{l.Blocks[0].ID, l.Blocks[1].ID}
		if got[0] != "b" || got[1] != "a" {
			t.Fatalf("expected [b a], got %v", got)
		}
	})

	t.Run("empty order is a no-op", func(t *testing.T) {
		l := Layout{Blocks: []Block{{ID: "a"}, {ID: "b"}}}
		if l.Reorder(nil) {
			t.Errorf("expected changed=false for empty order")
		}
		if l.Blocks[0].ID != "a" || l.Blocks[1].ID != "b" {
			t.Fatalf("blocks should be untouched, got %v", l.Blocks)
		}
	})
}

func TestLayoutState_LegacyMigration(t *testing.T) {
	// Legacy flat shape must migrate into Layouts[0].
	var s LayoutState
	if err := json.Unmarshal([]byte(`{"blocks":[{"id":"a","type":"stat","cols":1}]}`), &s); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if len(s.Layouts) != 1 || len(s.Layouts[0].Blocks) != 1 || s.Layouts[0].Blocks[0].ID != "a" {
		t.Fatalf("legacy migration failed, got %+v", s)
	}

	// New shape decodes directly.
	var s2 LayoutState
	if err := json.Unmarshal([]byte(`{"layouts":[{"blocks":[{"id":"x"}]},{"blocks":[]}]}`), &s2); err != nil {
		t.Fatalf("unmarshal new: %v", err)
	}
	if len(s2.Layouts) != 2 || s2.Layouts[0].Blocks[0].ID != "x" {
		t.Fatalf("new-shape decode failed, got %+v", s2)
	}
}

func TestLayoutState_ContentHelpers(t *testing.T) {
	s := LayoutState{Layouts: []Layout{
		{Blocks: []Block{{ID: "a"}}},
		{},
		{Blocks: []Block{{ID: "b"}}},
	}}
	flags := s.ContentFlags(3)
	if !flags[0] || flags[1] || !flags[2] {
		t.Fatalf("ContentFlags got %v", flags)
	}
	if s.UsedLayouts(3) != 2 {
		t.Fatalf("UsedLayouts expected 2, got %d", s.UsedLayouts(3))
	}
	if s.FirstUsedLayout(3) != 0 {
		t.Fatalf("FirstUsedLayout expected 0, got %d", s.FirstUsedLayout(3))
	}

	// EnsureLayouts pads, LayoutAt clamps out-of-range to 0.
	s.EnsureLayouts(5)
	if len(s.Layouts) != 5 {
		t.Fatalf("EnsureLayouts expected 5, got %d", len(s.Layouts))
	}
	if l := s.LayoutAt(99, 5); l != &s.Layouts[0] {
		t.Fatalf("LayoutAt out-of-range should clamp to layout 0")
	}
}

func TestClampCols(t *testing.T) {
	cases := []struct {
		n, max, want int
	}{
		{n: 0, max: 4, want: 1},
		{n: 1, max: 4, want: 1},
		{n: 3, max: 4, want: 3},
		{n: 5, max: 4, want: 4},
		{n: 99, max: 0, want: 1}, // bad max → treat as 1
		{n: 1, max: 0, want: 1},
	}
	for _, c := range cases {
		if got := clampCols(c.n, c.max); got != c.want {
			t.Errorf("clampCols(%d, %d) = %d; want %d", c.n, c.max, got, c.want)
		}
	}
}
