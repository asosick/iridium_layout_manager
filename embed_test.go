package layoutmgr

import (
	"strings"
	"testing"
)

func TestAssetVersion(t *testing.T) {
	jsVersion := assetVersion("layout_manager.js")
	if jsVersion == "" {
		t.Fatal("expected embedded JavaScript asset version")
	}
	if jsVersion == assetVersion("layout_manager.css") {
		t.Fatal("expected different assets to have different versions")
	}
	if got := assetVersion("../missing.js"); got != "" {
		t.Fatalf("expected no version for missing asset, got %q", got)
	}
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
	if !strings.Contains(source, "window.htmx.swap(grid, html") {
		t.Fatal("reorder should use the htmx swap lifecycle")
	}
	if strings.Contains(source, "grid.outerHTML = html") {
		t.Fatal("reorder should not bypass htmx with a direct outerHTML replacement")
	}
}

func TestZenModeRestoresThePageHeader(t *testing.T) {
	js, err := staticFS.ReadFile("static/layout_manager.js")
	if err != nil {
		t.Fatalf("read embedded JavaScript: %v", err)
	}
	source := string(js)
	if !strings.Contains(source, "destroy() {\n            this.setZenMode(false);") {
		t.Fatal("destroy should restore a page header hidden by Zen mode")
	}
	if !strings.Contains(source, "header.hidden = enabled") {
		t.Fatal("Zen mode should toggle the owning Iridium page header")
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

func TestDoneHandlerSupportsTeleportedToolbar(t *testing.T) {
	js, err := staticFS.ReadFile("static/layout_manager.js")
	if err != nil {
		t.Fatalf("read embedded JavaScript: %v", err)
	}
	source := string(js)
	if !strings.Contains(source, "finishEditing(e) {") {
		t.Fatal("Done should expose a handler within the teleported Alpine scope")
	}
	if strings.Contains(source, "this.$el.addEventListener('htmx:afterRequest'") {
		t.Fatal("Done should not rely on HTMX events bubbling into the pre-teleport root")
	}
}
