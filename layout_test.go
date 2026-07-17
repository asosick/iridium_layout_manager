package layoutmgr

import "testing"

func TestResizableBuilders(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	p.Resizable(false)
	if p.allowResize {
		t.Fatal("Resizable(false) should disable resizing")
	}
	p.Resizeable(true)
	if !p.allowResize {
		t.Fatal("deprecated Resizeable alias should remain compatible")
	}
}

func TestZenBuilder(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard").Zen()
	if !p.zenEnabled {
		t.Fatal("Zen should enable the reversible header control")
	}
}
