package layoutmgr

import (
	"embed"

	"github.com/iridiumgo/iridium/core/asset"
)

//go:embed static
var staticFS embed.FS

func init() {
	// Register globally so Alpine knows about the component before any HTMX
	// fragment containing x-data is scanned.
	asset.Register(
		asset.CSS("layout-manager", staticFS, "static/layout_manager.css").Package("layout-manager"),
		asset.JS("layout-manager", staticFS, "static/layout_manager.js").Package("layout-manager"),
	)
}
