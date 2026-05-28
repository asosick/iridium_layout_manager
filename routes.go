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
	routeRemove  = "/lm/remove" // ?id=...
	routeResize  = "/lm/resize" // ?id=...&cols=N
	routeReorder = "/lm/reorder"
	routeSave    = "/lm/save"
	routeAssets  = "/lm/static/" // serves /lm/static/<file>
)

func (p *LayoutManagerPage) registerLayoutRoutes(mux wrapper.IMux) {
	mux.HandleFunc(routeAdd, p.handleAdd)
	mux.HandleFunc(routeRemove, p.handleRemove)
	mux.HandleFunc(routeResize, p.handleResize)
	mux.HandleFunc(routeReorder, p.handleReorder)
	mux.HandleFunc(routeSave, p.handleSave)
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

	state := p.currentState(r)
	state.Blocks = append(state.Blocks, Block{
		ID:   uuid.NewString(),
		Type: key,
		Cols: 1,
	})
	p.persistAndRender(w, r, state)
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
	state := p.currentState(r)
	if !state.Remove(id) {
		// Block already gone (double-click); re-render anyway for idempotence.
	}
	p.persistAndRender(w, r, state)
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

	state := p.currentState(r)
	b, _ := state.Find(id)
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
	p.persistAndRender(w, r, state)
}

// handleReorder applies a new block order. Body is JSON `{"order":["id1","id2",...]}`
// (sent by the Alpine sort handler).
func (p *LayoutManagerPage) handleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r = attachWriter(r, w)

	var body struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	state := p.currentState(r)
	state.Reorder(body.Order)
	p.persistAndRender(w, r, state)
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
	if err := p.saveHook(r, state); err != nil {
		logger.Error("[layoutmgr] save failed: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	// Fire a client-side success notification via HX-Trigger.
	w.Header().Set("HX-Trigger", `{"notify":{"type":"success","message":"Layout saved","description":""}}`)
	// Re-render the grid so the response has a body to swap.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = p.renderGrid(w, r, state).Render(r.Context(), w)
}

// persistAndRender saves the (possibly-mutated) state under the default
// (session) store and writes the updated grid for an htmx swap. The user's
// custom SaveHook is intentionally NOT called here — that's only fired by
// the explicit "Save" button. Mutations are committed to the session so they
// survive within the user's working session.
func (p *LayoutManagerPage) persistAndRender(w http.ResponseWriter, r *http.Request, state LayoutState) {
	// Snap cols to current grid in case anything sneaks in out of bounds.
	for i := range state.Blocks {
		state.Blocks[i].Cols = clampCols(state.Blocks[i].Cols, p.gridCols)
	}

	// Always persist to the in-session store so the working view survives
	// page reloads. The custom SaveHook is reserved for explicit Save clicks.
	sessionKey := "layout:" + p.SlugStr
	_ = sessionSave(sessionKey)(r, state)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := p.renderGrid(w, r, state).Render(r.Context(), w); err != nil {
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
