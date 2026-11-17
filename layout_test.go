package layoutmgr

import (
	"testing"

	"github.com/a-h/templ"
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
