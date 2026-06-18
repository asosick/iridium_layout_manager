package layoutmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/google/uuid"
	"github.com/iridiumgo/iridium/core/context/ctxkeys"
	"github.com/iridiumgo/iridium/core/logger"
	"github.com/iridiumgo/iridium/network/wrapper"
)

// route suffixes — kept short because they live under the page slug.
const (
	routeAdd     = "/lm/add"
	routeRemove  = "/lm/remove"  // ?id=...&layout=N
	routeResize  = "/lm/resize"  // ?id=...&cols=N&layout=N
	routeReorder = "/lm/reorder" // ?layout=N
	routeSelect  = "/lm/select"  // ?layout=N — switch the displayed layout
	routeSave    = "/lm/save"
	routeAssets  = "/lm/static/" // serves /lm/static/<file>
)

func (p *LayoutManagerPage) registerLayoutRoutes(mux wrapper.IMux) {
	mux.HandleFunc(routeAdd, p.handleAdd)
	mux.HandleFunc(routeRemove, p.handleRemove)
	mux.HandleFunc(routeResize, p.handleResize)
	mux.HandleFunc(routeReorder, p.handleReorder)
	mux.HandleFunc(routeSelect, p.handleSelect)
	mux.HandleFunc(routeSave, p.handleSave)
}

// layoutIndex reads the target layout index from the request (form value first,
// then query) and clamps it into [0, layoutCount). Missing/garbage → 0. Every
// mutation is scoped to one layout, so this picks which one.
func (p *LayoutManagerPage) layoutIndex(r *http.Request) int {
	raw := r.FormValue("layout")
	if raw == "" {
		raw = r.URL.Query().Get("layout")
	}
	i, err := strconv.Atoi(raw)
	if err != nil || i < 0 || i >= p.layoutCount {
		return 0
	}
	return i
}

// handleAdd appends a new block of the requested type. POST body / form:
// `type=<block-key>`. Responds with the re-rendered grid for htmx swap.
func (p *LayoutManagerPage) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = attachWriter(r, w)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	key := r.FormValue("type")
	spec := p.blockByKey(key)
	if spec == nil {
		http.Error(w, "unknown block type", http.StatusBadRequest)
		return
	}

	idx := p.layoutIndex(r)
	state := p.currentState(r)
	layout := state.LayoutAt(idx, p.layoutCount)
	layout.Blocks = append(layout.Blocks, Block{
		ID:   uuid.NewString(),
		Type: key,
		Cols: 1,
	})
	p.persistAndRender(w, r, state, idx)
}

// handleRemove drops a block by ID.
func (p *LayoutManagerPage) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = attachWriter(r, w)

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	idx := p.layoutIndex(r)
	state := p.currentState(r)
	// Remove(id) returning false just means the block is already gone
	// (double-click); re-render anyway for idempotence.
	state.LayoutAt(idx, p.layoutCount).Remove(id)
	p.persistAndRender(w, r, state, idx)
}

// handleResize updates a block's column span. Query: ?id=...&cols=N. cols=0
// is taken as a "toggle" between 1 and GridColumns (the filament behaviour).
func (p *LayoutManagerPage) handleResize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = attachWriter(r, w)

	id := r.URL.Query().Get("id")
	colsStr := r.URL.Query().Get("cols")
	deltaStr := r.URL.Query().Get("delta")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	idx := p.layoutIndex(r)
	state := p.currentState(r)
	b, _ := state.LayoutAt(idx, p.layoutCount).Find(id)
	if b == nil {
		http.Error(w, "unknown block", http.StatusBadRequest)
		return
	}
	switch {
	case deltaStr != "":
		delta, err := strconv.Atoi(deltaStr)
		if err != nil {
			http.Error(w, "bad delta", http.StatusBadRequest)
			return
		}
		b.Cols = clampCols(b.Cols+delta, p.gridCols)
	case colsStr != "":
		cols, err := strconv.Atoi(colsStr)
		if err != nil {
			http.Error(w, "bad cols", http.StatusBadRequest)
			return
		}
		if cols == 0 {
			// Toggle full ↔ 1 (filament behaviour).
			if b.Cols == p.gridCols {
				b.Cols = 1
			} else {
				b.Cols = p.gridCols
			}
		} else {
			b.Cols = clampCols(cols, p.gridCols)
		}
	default:
		http.Error(w, "missing cols or delta", http.StatusBadRequest)
		return
	}
	p.persistAndRender(w, r, state, idx)
}

// handleReorder applies a new block order. Body is JSON `{"order":["id1","id2",...]}`
// (sent by the Alpine sort handler).
func (p *LayoutManagerPage) handleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = attachWriter(r, w)

	idx := p.layoutIndex(r)
	var body struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	state := p.currentState(r)
	state.LayoutAt(idx, p.layoutCount).Reorder(body.Order)
	p.persistAndRender(w, r, state, idx)
}

// handleSelect re-renders the grid for a different layout (page). No mutation —
// it's a pure GET that swaps the grid to show the requested layout's blocks.
// The client (Alpine) tracks which layout is current; this keeps the server's
// rendered URLs (add/remove/resize target query strings) in sync with it.
func (p *LayoutManagerPage) handleSelect(w http.ResponseWriter, r *http.Request) {
	r = attachWriter(r, w)
	idx := p.layoutIndex(r)
	state := p.currentState(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := p.renderGrid(w, r, state, idx).Render(r.Context(), w); err != nil {
		logger.Error("[layoutmgr] render grid (select): %v", err)
	}
}

// handleSave fires the consumer's SaveHook (or just re-saves the session
// state) and emits an iridium notification on success.
func (p *LayoutManagerPage) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = attachWriter(r, w)

	state := p.currentState(r)
	if err := p.sessionSave(r, state); err != nil {
		logger.Error("[layoutmgr] save failed: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	if p.saveHook != nil {
		if err := p.saveHook(r, state); err != nil {
			logger.Error("[layoutmgr] save hook failed: %v", err)
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
	}
	// Every mutation (add/remove/resize/reorder) already persists immediately
	// through persistAndRender, so an explicit Save only needs to commit and
	// confirm — there's nothing new to render. Re-rendering the grid here and
	// swapping it back in would tear down and rebuild every widget (resetting
	// per-instance front-end state, scroll positions, etc.) for no benefit, so
	// we send no body (the button uses hx-swap="none") and just fire the
	// success notification via HX-Trigger.
	w.Header().Set("HX-Trigger", `{"notify":{"type":"success","message":"Layout saved","description":""}}`)
	w.WriteHeader(http.StatusNoContent)
}

// persistAndRender stores working changes in the user's session and writes the
// updated grid for an htmx swap. Durable persistence remains an explicit Save.
func (p *LayoutManagerPage) persistAndRender(w http.ResponseWriter, r *http.Request, state LayoutState, layoutIdx int) {
	// Snap cols to current grid in case anything sneaks in out of bounds.
	for li := range state.Layouts {
		blocks := state.Layouts[li].Blocks
		for i := range blocks {
			blocks[i].Cols = clampCols(blocks[i].Cols, p.gridCols)
		}
	}

	if err := p.sessionSave(r, state); err != nil {
		logger.Error("[layoutmgr] persist mutation failed: %v", err)
		http.Error(w, "failed to persist layout", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := p.renderGrid(w, r, state, layoutIdx).Render(r.Context(), w); err != nil {
		logger.Error("[layoutmgr] render grid: %v", err)
	}
}

// --- asset routes -----------------------------------------------------------

func (p *LayoutManagerPage) registerAssetRoutes(mux wrapper.IMux) {
	// We can't use http.StripPrefix easily because iridium's mux pattern is
	// scoped to the page slug — register one handler per file. This is a
	// tiny number of assets (CSS + JS), so explicit is fine and safer than
	// path concatenation tricks.
	for _, name := range assetNames() {
		name := name
		mux.HandleFunc(routeAssets+name, func(w http.ResponseWriter, r *http.Request) {
			serveAsset(w, r, name)
		})
	}
}

// --- helpers ----------------------------------------------------------------

// assetURL returns the URL a templ component should reference for one of the
// embedded assets, accounting for the panel + page slug prefix discoverable
// from the live request. Without the panel path, we'd get a 404 because
// iridium routes the request inside a panel-prefixed mux.
func (p *LayoutManagerPage) assetURL(r *http.Request, name string) string {
	prefix := panelPathFromRequest(r)
	return prefix + p.SlugStr + routeAssets + name
}

// panelPathFromRequest pulls the panel prefix from the current request's
// context. Iridium sets ctxkeys.PanelPath on every request that goes through
// a panel mux; if missing we fall back to "" (root-mounted page).
func panelPathFromRequest(r *http.Request) string {
	v := r.Context().Value(ctxkeys.PanelPath)
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	// Defensive: ensure no trailing slash so concatenation is clean.
	return strings.TrimRight(s, "/")
}

// usePanelPath sets the panel path on a context for tests / standalone
// rendering (unit tests don't spin up a full panel). Kept exported-internally
// for future test packages.
func usePanelPath(parent context.Context, prefix string) context.Context {
	return context.WithValue(parent, ctxkeys.PanelPath, prefix)
}

var _ templ.Component = templ.NopComponent
