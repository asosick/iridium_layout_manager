package layoutmgr

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/a-h/templ"
	"github.com/goiridium/iridium/core/context/ctxkeys"
	"github.com/goiridium/iridium/core/logger"
	"github.com/goiridium/iridium/network/wrapper"
	"github.com/google/uuid"

	"github.com/asosick/iridium_layout_manager/views"
)

// route suffixes — kept short because they live under the page slug.
const (
	routeAdd     = "/lm/add"     // ?type=...&layout=N → the new block
	routeRemove  = "/lm/remove"  // ?id=...&layout=N → 204
	routeResize  = "/lm/resize"  // ?id=...&cols=N|delta=±1&layout=N → 204, X-Layout-Manager-Cols
	routeMove    = "/lm/move"    // ?id=...&delta=±1&layout=N → 204
	routeReorder = "/lm/reorder" // ?layout=N, form order=id… → 204
	routeSelect  = "/lm/select"  // ?layout=N&mode=view|edit → the grid
	routeSave    = "/lm/save"    // layout=N → 204
	routeReset   = "/lm/reset"   // ?layout=N → the grid

	routeAddPage    = "/lm/pages/add"    // → the new page's grid
	routeRemovePage = "/lm/pages/remove" // ?layout=N → the grid of the page before
	routeRenamePage = "/lm/pages/rename" // ?layout=N, form name=… → the name kept

	nextLayoutHeader = "X-Iridium-Layout-Manager-Next-Layout"
	colsHeader       = "X-Layout-Manager-Cols"
)

func (p *LayoutManagerPage) registerLayoutRoutes(mux wrapper.IMux) {
	mux.HandleFunc(routeAdd, p.handleAdd)
	mux.HandleFunc(routeRemove, p.handleRemove)
	mux.HandleFunc(routeResize, p.handleResize)
	mux.HandleFunc(routeMove, p.handleMove)
	mux.HandleFunc(routeReorder, p.handleReorder)
	mux.HandleFunc(routeSelect, p.handleSelect)
	mux.HandleFunc(routeSave, p.handleSave)
	mux.HandleFunc(routeReset, p.handleReset)
	mux.HandleFunc(routeAddPage, p.handleAddPage)
	mux.HandleFunc(routeRemovePage, p.handleRemovePage)
	mux.HandleFunc(routeRenamePage, p.handleRenamePage)
}

var errBadLayout = errors.New("invalid layout")

// layoutIndex reads the target layout index from the request (form value,
// which includes the query). Absent means 0; anything that cannot be one of
// the page's layouts is an error, so a stale or wrong request edits nothing.
// When the user adds pages, whether the index names one of theirs is known
// once their layouts are loaded (see editState).
func (p *LayoutManagerPage) layoutIndex(r *http.Request) (int, error) {
	raw := r.FormValue("layout")
	if raw == "" {
		return 0, nil
	}
	i, err := strconv.Atoi(raw)
	if err != nil || i < 0 || (p.fixedPages() && i >= p.layoutCount) {
		return 0, errBadLayout
	}
	return i, nil
}

func allowPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// edit runs fn on the layout the request names, under the user's lock, so the
// user's requests apply one after another, each to the result of the last.
// fn answers the request itself when it fails (and returns false).
func (p *LayoutManagerPage) edit(w http.ResponseWriter, r *http.Request, fn func(l *Layout) bool) (LayoutState, int, bool) {
	return p.editState(w, r, func(state *LayoutState, idx int) bool {
		return fn(&state.Layouts[idx])
	})
}

// editState is edit for changes to the pages themselves: fn gets the whole
// draft and the index of the page the request names, which exists.
func (p *LayoutManagerPage) editState(w http.ResponseWriter, r *http.Request, fn func(state *LayoutState, idx int) bool) (LayoutState, int, bool) {
	idx, err := p.layoutIndex(r)
	if err != nil {
		http.Error(w, "invalid layout", http.StatusBadRequest)
		return LayoutState{}, 0, false
	}
	u := p.owner(w, r)
	u.mu.Lock()
	defer u.mu.Unlock()
	state, err := p.draftState(r, u)
	if err != nil {
		logger.Error("[layoutmgr] load layout: %v", err)
		http.Error(w, "failed to load layout", http.StatusInternalServerError)
		return state, idx, false
	}
	if idx >= p.pageCount(state) {
		http.Error(w, "invalid layout", http.StatusBadRequest)
		return state, idx, false
	}
	if !fn(&state, idx) {
		return state, idx, false
	}
	state = p.normalize(state)
	draft := clone(state)
	u.draft = &draft
	return state, idx, true
}

// handleAdd appends a new block of the requested type and answers it, for
// the client to append to the grid. Form or query: type=<block-key>&layout=N.
func (p *LayoutManagerPage) handleAdd(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	key := r.FormValue("type")
	spec := p.blockByKey(r, key)
	if spec == nil {
		http.Error(w, "unknown block type", http.StatusBadRequest)
		return
	}
	block := Block{ID: uuid.NewString(), Type: key, Cols: 1}
	state, idx, ok := p.edit(w, r, func(l *Layout) bool {
		l.Blocks = append(l.Blocks, block)
		return true
	})
	if !ok {
		return
	}
	d := p.pageData(w, r, state, idx)
	blocks := state.Layouts[idx].Blocks
	p.writeHTML(w, r, views.Block(d, p.blockData(w, r, d, spec, block, len(blocks)-1, len(blocks))))
}

// handleRemove drops a block by ID. Removing a block that is already gone (a
// double click) succeeds.
func (p *LayoutManagerPage) handleRemove(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if _, _, ok := p.edit(w, r, func(l *Layout) bool {
		l.Remove(id)
		return true
	}); ok {
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleResize updates a block's column span and answers the new span in a
// header. Query: ?id=...&cols=N, or delta=±1. cols=0 toggles between 1 and
// GridColumns (the filament behaviour).
func (p *LayoutManagerPage) handleResize(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	q := r.URL.Query()
	id, colsStr, deltaStr := q.Get("id"), q.Get("cols"), q.Get("delta")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	cols := 0
	_, _, ok := p.edit(w, r, func(l *Layout) bool {
		b, _ := l.Find(id)
		if b == nil {
			http.Error(w, "unknown block", http.StatusBadRequest)
			return false
		}
		switch {
		case deltaStr != "":
			delta, err := strconv.Atoi(deltaStr)
			if err != nil {
				http.Error(w, "bad delta", http.StatusBadRequest)
				return false
			}
			b.Cols = clampCols(b.Cols+delta, p.gridCols)
		case colsStr != "":
			n, err := strconv.Atoi(colsStr)
			if err != nil {
				http.Error(w, "bad cols", http.StatusBadRequest)
				return false
			}
			if n == 0 {
				// Toggle full ↔ 1 (filament behaviour).
				if clampCols(b.Cols, p.gridCols) == p.gridCols {
					b.Cols = 1
				} else {
					b.Cols = p.gridCols
				}
			} else {
				b.Cols = clampCols(n, p.gridCols)
			}
		default:
			http.Error(w, "missing cols or delta", http.StatusBadRequest)
			return false
		}
		cols = b.Cols
		return true
	})
	if ok {
		w.Header().Set(colsHeader, strconv.Itoa(cols))
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMove shifts one block left or right by one position. The endpoint is
// separate from drag reorder so keyboard and pointer users get the same
// deterministic operation.
func (p *LayoutManagerPage) handleMove(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	delta, err := strconv.Atoi(r.URL.Query().Get("delta"))
	if id == "" || err != nil || (delta != -1 && delta != 1) {
		http.Error(w, "invalid move", http.StatusBadRequest)
		return
	}
	if _, _, ok := p.edit(w, r, func(l *Layout) bool {
		l.Move(id, delta)
		return true
	}); ok {
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleReorder applies a new block order, sent as form values:
// order=id1&order=id2…
func (p *LayoutManagerPage) handleReorder(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	order := r.PostForm["order"]
	if _, _, ok := p.edit(w, r, func(l *Layout) bool {
		l.Reorder(order)
		return true
	}); ok {
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleSelect renders the grid of one layout (page): the draft with
// mode=edit, else the committed layout. Nothing changes.
func (p *LayoutManagerPage) handleSelect(w http.ResponseWriter, r *http.Request) {
	idx, err := p.layoutIndex(r)
	if err != nil {
		http.Error(w, "invalid layout", http.StatusBadRequest)
		return
	}
	editing := r.FormValue("mode") == "edit" || !p.showLockBtn
	state, unsaved, err := p.shownState(w, r, editing)
	if err != nil {
		logger.Error("[layoutmgr] load layout: %v", err)
		http.Error(w, "failed to load layout", http.StatusInternalServerError)
		return
	}
	r = withCurrentPageQuery(r)
	p.writeHTML(w, r, p.renderGrid(w, r, state, idx, unsaved))
}

// withCurrentPageQuery gives widgets the page URL state that caused this HTMX
// partial render. Selector request values win so the requested layout cannot be
// replaced by a stale value from the browser URL.
func withCurrentPageQuery(r *http.Request) *http.Request {
	currentURL := r.Header.Get("HX-Current-URL")
	if currentURL == "" {
		return r
	}

	parsed, err := url.Parse(currentURL)
	if err != nil {
		return r
	}

	query := parsed.Query()
	for key, values := range r.URL.Query() {
		query.Del(key)
		for _, value := range values {
			query.Add(key, value)
		}
	}

	clone := r.Clone(r.Context())
	cloneURL := *r.URL
	cloneURL.RawQuery = query.Encode()
	clone.URL = &cloneURL
	return clone
}

// handleSave commits the draft when Done is clicked: the SaveHook first, and
// nothing is committed if it fails.
func (p *LayoutManagerPage) handleSave(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	current, err := p.layoutIndex(r)
	if err != nil {
		http.Error(w, "invalid layout", http.StatusBadRequest)
		return
	}
	u := p.owner(w, r)
	u.mu.Lock()
	defer u.mu.Unlock()
	state, err := p.draftState(r, u)
	if err != nil {
		logger.Error("[layoutmgr] load layout: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	if p.saveHook != nil {
		// The hook gets a copy of its own, which the application may keep.
		if err := p.saveHook(r, clone(state)); err != nil {
			logger.Error("[layoutmgr] save hook failed: %v", err)
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
	}
	committed := clone(state)
	u.committed = &committed
	u.draft = nil

	count := p.pageCount(state)
	if current >= count || !state.ContentFlags(count)[current] {
		if next, ok := state.NextPopulatedLayout(current, count); ok {
			w.Header().Set(nextLayoutHeader, strconv.Itoa(next))
		}
	}
	w.Header().Set("HX-Trigger", `{"notify":{"type":"success","message":"Layout saved","description":""}}`)
	w.WriteHeader(http.StatusNoContent)
}

// handleAddPage adds an empty page after the user's last and answers its
// grid, for the client to show. Only when LayoutCount leaves the number of
// pages to the user.
func (p *LayoutManagerPage) handleAddPage(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	if p.fixedPages() {
		http.Error(w, "this page has a fixed number of pages", http.StatusBadRequest)
		return
	}
	added := 0
	state, _, ok := p.editState(w, r, func(state *LayoutState, _ int) bool {
		state.Layouts = append(state.Layouts, Layout{})
		added = len(state.Layouts) - 1
		return true
	})
	if ok {
		p.writeHTML(w, r, p.renderGrid(w, r, state, added, true))
	}
}

// handleRemovePage removes the page ?layout=N names, its blocks with it, and
// answers the grid of the page before it. The last page stays.
func (p *LayoutManagerPage) handleRemovePage(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	if p.fixedPages() {
		http.Error(w, "this page has a fixed number of pages", http.StatusBadRequest)
		return
	}
	shown := 0
	state, _, ok := p.editState(w, r, func(state *LayoutState, idx int) bool {
		if len(state.Layouts) <= 1 {
			http.Error(w, "the last page cannot be removed", http.StatusBadRequest)
			return false
		}
		state.RemoveLayout(idx)
		shown = max(0, idx-1)
		return true
	})
	if ok {
		p.writeHTML(w, r, p.renderGrid(w, r, state, shown, true))
	}
}

// handleRenamePage names the page ?layout=N (form value name; empty shows
// its number again) and answers the name kept, tidied (see pageName).
func (p *LayoutManagerPage) handleRenamePage(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	if !p.allowRename {
		http.Error(w, "pages cannot be renamed here", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := pageName(r.PostFormValue("name"))
	if _, _, ok := p.edit(w, r, func(l *Layout) bool {
		l.Name = name
		return true
	}); ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, name)
	}
}

// pageName tidies a page name as typed: single spaces between its words and
// none around them, no other control characters, and at most
// views.PageNameLength characters.
func pageName(typed string) string {
	name := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, typed)), " ")
	if runes := []rune(name); len(runes) > views.PageNameLength {
		name = strings.TrimSpace(string(runes[:views.PageNameLength]))
	}
	return name
}

// handleReset discards the draft and answers the committed layout's grid.
// With hooks it is read back from the application's storage.
func (p *LayoutManagerPage) handleReset(w http.ResponseWriter, r *http.Request) {
	if !allowPost(w, r) {
		return
	}
	idx, err := p.layoutIndex(r)
	if err != nil {
		http.Error(w, "invalid layout", http.StatusBadRequest)
		return
	}
	u := p.owner(w, r)
	u.mu.Lock()
	u.draft = nil
	state, err := p.committedState(r, u)
	u.mu.Unlock()
	if err != nil {
		logger.Error("[layoutmgr] reset: %v", err)
		http.Error(w, "reset failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", `{"notify":{"type":"success","message":"Layout reset","description":""}}`)
	p.writeHTML(w, r, p.renderGrid(w, r, state, idx, false))
}

func (p *LayoutManagerPage) writeHTML(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		logger.Error("[layoutmgr] render: %v", err)
	}
}

// --- helpers ----------------------------------------------------------------

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
