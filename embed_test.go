package layoutmgr

import (
	"strings"
	"testing"

	"github.com/iridiumgo/iridium/core/asset"
)

func TestAssetsRegisterWithIridium(t *testing.T) {
	// Registered assets carry a ?v=<content hash> query since Iridium alpha.21.
	if got := asset.StyleHref("layout-manager", "layout-manager"); !isVersionedURL(got, "/static/registered/layout-manager/layout-manager.css") {
		t.Fatalf("unexpected stylesheet URL %q", got)
	}
	if got := asset.ScriptSrc("layout-manager", "layout-manager"); !isVersionedURL(got, "/static/registered/layout-manager/layout-manager.js") {
		t.Fatalf("unexpected script URL %q", got)
	}
}

func isVersionedURL(got, path string) bool {
	p, query, _ := strings.Cut(got, "?")
	return p == path && strings.HasPrefix(query, "v=") && len(query) > len("v=")
}

func TestResizeGuideDoesNotExposeMasonryRows(t *testing.T) {
	css, err := staticFS.ReadFile("static/layout_manager.css")
	if err != nil {
		t.Fatalf("read embedded CSS: %v", err)
	}
	if strings.Contains(string(css), "repeating-linear-gradient") {
		t.Fatal("resize guide should show columns, not internal masonry rows")
	}
}

func TestReorderUsesHTMXSwapLifecycle(t *testing.T) {
	js, err := staticFS.ReadFile("static/layout_manager.js")
	if err != nil {
		t.Fatalf("read embedded JavaScript: %v", err)
	}
	source := string(js)
	if !strings.Contains(source, "window.htmx.swap({") {
		t.Fatal("reorder should use the htmx swap lifecycle")
	}
	if strings.Contains(source, "grid.outerHTML = html") {
		t.Fatal("reorder should not bypass htmx with a direct outerHTML replacement")
	}
}

func TestZenModeUsesNativePageState(t *testing.T) {
	js, err := staticFS.ReadFile("static/layout_manager.js")
	if err != nil {
		t.Fatalf("read embedded JavaScript: %v", err)
	}
	source := string(js)
	if !strings.Contains(source, "setZenMode(enabled) {\n            this.zenMode = enabled;") {
		t.Fatal("Zen mode should update the native page controller state")
	}
	for _, forbidden := range []string{"header.hidden", "_pageHeader", "closest('.ir-panel-page')"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Zen mode should not discover or mutate the page header with %q", forbidden)
		}
	}
}

func TestZenModeCapturesEscapeAndUsesAnOpaqueExitControl(t *testing.T) {
	js, err := staticFS.ReadFile("static/layout_manager.js")
	if err != nil {
		t.Fatalf("read embedded JavaScript: %v", err)
	}
	source := string(js)
	for _, expected := range []string{
		"window.addEventListener('keydown', this._zenEscapeHandler, true)",
		"window.removeEventListener('keydown', this._zenEscapeHandler, true)",
		"e.stopPropagation()",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Zen Escape handling should contain %q", expected)
		}
	}

	css, err := staticFS.ReadFile("static/layout_manager.css")
	if err != nil {
		t.Fatalf("read embedded CSS: %v", err)
	}
	if !strings.Contains(string(css), "background: var(--color-background);") {
		t.Fatal("Exit Zen should use an opaque theme background")
	}
}

func TestDoneHandlerUsesNativeToolbar(t *testing.T) {
	js, err := staticFS.ReadFile("static/layout_manager.js")
	if err != nil {
		t.Fatalf("read embedded JavaScript: %v", err)
	}
	source := string(js)
	if !strings.Contains(source, "finishEditing(e) {") {
		t.Fatal("Done should expose a handler within the native page Alpine scope")
	}
	if strings.Contains(source, "this.$el.addEventListener('htmx:afterRequest'") {
		t.Fatal("Done should use the control's completed request event")
	}
	for _, expected := range []string{
		"get('X-Iridium-Layout-Manager-Next-Layout')",
		"Number.isInteger(next)",
		"next !== this.currentLayout",
		"this.selectLayout(next)",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Done should follow the server-selected populated layout using %q", expected)
		}
	}
	for _, stale := range []string{"selectNextPopulatedLayout", "data-lm-has-content"} {
		if strings.Contains(source, stale) {
			t.Fatalf("Done should not infer saved layout state from stale DOM metadata %q", stale)
		}
	}
}
