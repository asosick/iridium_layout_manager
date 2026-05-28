package layoutmgr

import "testing"

func TestLayoutState_FindAndRemove(t *testing.T) {
	s := LayoutState{Blocks: []Block{
		{ID: "a", Type: "stat", Cols: 1},
		{ID: "b", Type: "chart", Cols: 2},
		{ID: "c", Type: "stat", Cols: 1},
	}}

	b, i := s.Find("b")
	if i != 1 || b == nil || b.Type != "chart" {
		t.Fatalf("Find(b): expected index 1 / chart, got i=%d b=%v", i, b)
	}

	if got, _ := s.Find("zzz"); got != nil {
		t.Errorf("Find for missing id should be nil, got %v", got)
	}

	if !s.Remove("b") {
		t.Fatalf("Remove(b) returned false")
	}
	if len(s.Blocks) != 2 || s.Blocks[0].ID != "a" || s.Blocks[1].ID != "c" {
		t.Fatalf("after Remove(b), expected [a c], got %v", s.Blocks)
	}

	if s.Remove("zzz") {
		t.Errorf("Remove(missing) should return false")
	}
}

func TestLayoutState_Reorder(t *testing.T) {
	t.Run("full reorder", func(t *testing.T) {
		s := LayoutState{Blocks: []Block{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
		if !s.Reorder([]string{"c", "a", "b"}) {
			t.Fatalf("expected reorder to report changed=true")
		}
		got := []string{s.Blocks[0].ID, s.Blocks[1].ID, s.Blocks[2].ID}
		if got[0] != "c" || got[1] != "a" || got[2] != "b" {
			t.Fatalf("expected [c a b], got %v", got)
		}
	})

	t.Run("partial order appends missing", func(t *testing.T) {
		s := LayoutState{Blocks: []Block{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
		s.Reorder([]string{"c"})
		got := []string{s.Blocks[0].ID, s.Blocks[1].ID, s.Blocks[2].ID}
		if got[0] != "c" || got[1] != "a" || got[2] != "b" {
			t.Fatalf("expected [c a b], got %v", got)
		}
	})

	t.Run("unknown ids ignored", func(t *testing.T) {
		s := LayoutState{Blocks: []Block{{ID: "a"}, {ID: "b"}}}
		s.Reorder([]string{"zzz", "b", "yyy"})
		got := []string{s.Blocks[0].ID, s.Blocks[1].ID}
		if got[0] != "b" || got[1] != "a" {
			t.Fatalf("expected [b a], got %v", got)
		}
	})

	t.Run("empty order is a no-op", func(t *testing.T) {
		s := LayoutState{Blocks: []Block{{ID: "a"}, {ID: "b"}}}
		if s.Reorder(nil) {
			t.Errorf("expected changed=false for empty order")
		}
		if s.Blocks[0].ID != "a" || s.Blocks[1].ID != "b" {
			t.Fatalf("blocks should be untouched, got %v", s.Blocks)
		}
	})
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
