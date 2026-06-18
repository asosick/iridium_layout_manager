package layoutmgr

import "testing"

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
