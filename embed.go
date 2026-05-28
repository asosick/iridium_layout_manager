package layoutmgr

import (
	"embed"
	"io"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var staticFS embed.FS

// assetNames returns the file names (without the "static/" prefix) the plugin
// ships, so registerAssetRoutes can register a handler per file.
func assetNames() []string {
	entries, err := staticFS.ReadDir("static")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// serveAsset streams one of the embedded files with a reasonable Content-Type
// and a one-hour cache. Files are tiny so we just read them whole.
func serveAsset(w http.ResponseWriter, _ *http.Request, name string) {
	// Strip any sneaky path components — name comes from a route pattern we
	// control but defence-in-depth is cheap.
	name = path.Base(name)
	f, err := staticFS.Open("static/" + name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", contentTypeFor(name))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.Copy(w, f)
}

func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
