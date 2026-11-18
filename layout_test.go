package layoutmgr

import (
	"testing"

	"github.com/a-h/templ"
	"github.com/goiridium/iridium/core/context/ctxPanel"
	"github.com/goiridium/iridium/core/enum"
)

func TestNativePageExtensionBuildersRemainFluent(t *testing.T) {
	header := templ.Raw(`<div data-test-header></div>`)
	p := NewLayoutManagerPage("Dashboard", "dashboard").
		Attributes(templ.Attributes{"data-page": "dashboard"}).
		HeaderAttributes(templ.Attributes{"data-header": "dashboard"}).
		HeaderComponents(header).
		Zen()

	if p.AttributesObj["data-page"] != "dashboard" || p.HeaderAttributesObj["data-header"] != "dashboard" {
		t.Fatal("native page extension attributes were not stored")
	}
	if len(p.HeaderComponentsArr) != 1 || p.HeaderComponentsArr[0] == nil {
		t.Fatalf("header components = %#v", p.HeaderComponentsArr)
	}
}

func TestCustomPageCloneOwnsItsResolvableState(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard").
		Attributes(templ.Attributes{"data-page": "shared"})

	cloned := p.cloneCustomPanelPage()
	cloned.AttributesObj = templ.Attributes{"data-page": "request"}

	if p.AttributesObj["data-page"] != "shared" {
		t.Fatal("per-request page attributes mutated the shared page definition")
	}
	if cloned.PanelPageResolvable == p.PanelPageResolvable {
		t.Fatal("per-request page clone shares its resolvable state")
	}
}

// The vertical padding builders keep the chain going and reach the copy of
// the page each request renders.
func TestVerticalPaddingBuildersReachEachRequest(t *testing.T) {
	none := func(*ctxPanel.PanelPage) enum.PageVerticalPadding { return enum.PageVerticalPaddingNone }
	for name, tc := range map[string]struct {
		build func(*LayoutManagerPage) *LayoutManagerPage
		want  enum.PageVerticalPadding
	}{
		"compact": {(*LayoutManagerPage).CompactVerticalPadding, enum.PageVerticalPaddingCompact},
		"none":    {(*LayoutManagerPage).NoVerticalPadding, enum.PageVerticalPaddingNone},
		"value": {func(p *LayoutManagerPage) *LayoutManagerPage {
			return p.VerticalPadding(enum.PageVerticalPaddingCompact)
		}, enum.PageVerticalPaddingCompact},
		"callback": {func(p *LayoutManagerPage) *LayoutManagerPage { return p.VerticalPaddingFn(none) }, enum.PageVerticalPaddingNone},
	} {
		p := tc.build(NewLayoutManagerPage("Dashboard", "dashboard")).GridColumns(3)
		perRequest := p.cloneCustomPanelPage()
		if got := perRequest.VerticalPaddingStr.Get(nil); got != tc.want {
			t.Errorf("%s: a request renders vertical padding %q, want %q", name, got, tc.want)
		}
	}
}
