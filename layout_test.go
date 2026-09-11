package layoutmgr

import (
	"testing"

	"github.com/a-h/templ"
)

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
