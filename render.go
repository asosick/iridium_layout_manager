package layoutmgr

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/a-h/templ"
	"github.com/iridiumgo/iridium/core/context/ctxkeys"

	"github.com/asosick/iridium_layout_manager/views"
)

// buildInitialPageData builds the page for a full page render: the layout
// named by ?layout=N (so a reload stays on the page the user was on), else
// the first layout with content.
func (p *LayoutManagerPage) buildInitialPageData(w http.ResponseWriter, r *http.Request, state LayoutState, unsaved bool) *views.PageData {
	current := state.FirstUsedLayout(p.layoutCount)
	if raw := r.URL.Query().Get("layout"); raw != "" {
		if i, err := strconv.Atoi(raw); err == nil && i >= 0 && i < p.layoutCount {
			current = i
		}
	}
	d := p.pageData(w, r, state, current)
	d.Unsaved = unsaved
	d.Blocks = p.renderBlocks(w, r, d, state.LayoutAt(current, p.layoutCount).Blocks)
	return d
}

// renderGrid returns the grid of one layout (the swap target for page
// switches and Reset).
func (p *LayoutManagerPage) renderGrid(w http.ResponseWriter, r *http.Request, state LayoutState, currentLayout int, unsaved bool) templ.Component {
	d := p.pageData(w, r, state, currentLayout)
	d.Unsaved = unsaved
	d.Blocks = p.renderBlocks(w, r, d, state.LayoutAt(currentLayout, p.layoutCount).Blocks)
	return views.Grid(d)
}

// pageData populates the view layer's PageData from the plugin's
// configuration and the state shown, without rendering any block.
func (p *LayoutManagerPage) pageData(w http.ResponseWriter, r *http.Request, state LayoutState, currentLayout int) *views.PageData {
	prefix := panelPathFromRequest(r) + p.SlugStr

	if currentLayout < 0 || currentLayout >= p.layoutCount {
		currentLayout = 0
	}

	d := &views.PageData{
		GridColumns:    p.gridCols,
		ShowLockButton: p.showLockBtn,
		AllowReorder:   p.allowReorder,
		AllowResize:    p.allowResize,
		ZenEnabled:     p.zenEnabled,
		LayoutCount:    p.layoutCount,
		CurrentLayout:  currentLayout,
		LayoutContent:  state.ContentFlags(p.layoutCount),
		UsedLayouts:    state.UsedLayouts(p.layoutCount),
		Request:        r,
		Writer:         w,
		URLs: views.PageURLs{
			Add:     prefix + routeAdd,
			Remove:  prefix + routeRemove,
			Resize:  prefix + routeResize,
			Move:    prefix + routeMove,
			Reorder: prefix + routeReorder,
			Select:  prefix + routeSelect,
			Save:    prefix + routeSave,
			Reset:   prefix + routeReset,
		},
	}

	// Dropdown options come straight from the specs on offer; the source is
	// asked once per build.
	specs := p.specs(r)
	d.AddOptions = make([]views.BlockOption, 0, len(specs))
	for _, b := range specs {
		d.AddOptions = append(d.AddOptions, views.BlockOption{Key: b.Key(), Label: b.Label()})
	}
	d.ReorderURL = d.URLs.Reorder + "?layout=" + strconv.Itoa(currentLayout)
	return d
}

// renderBlocks resolves the render data of the given blocks, which make up
// the whole of the shown layout.
func (p *LayoutManagerPage) renderBlocks(w http.ResponseWriter, r *http.Request, d *views.PageData, blocks []Block) []views.BlockData {
	// The source is asked once per render, not once per block.
	byKey := make(map[string]BlockSpec)
	for _, spec := range p.specs(r) {
		if _, dup := byKey[spec.Key()]; !dup {
			byKey[spec.Key()] = spec
		}
	}
	out := make([]views.BlockData, 0, len(blocks))
	for i, b := range blocks {
		out = append(out, p.blockData(w, r, d, byKey[b.Type], b, i, len(blocks)))
	}
	return out
}

// blockData resolves one block: the block at index of count in the shown
// layout. A block whose spec was removed since it was placed renders as a
// "missing" placeholder rather than failing the page.
//
// Every per-block URL carries &layout=N so an edit lands on the layout the
// grid is showing.
func (p *LayoutManagerPage) blockData(w http.ResponseWriter, r *http.Request, d *views.PageData, spec BlockSpec, b Block, index, count int) views.BlockData {
	var comp templ.Component
	if spec == nil {
		comp = views.MissingBlock(b.Type)
	} else {
		// Give every block instance a unique front-end id derived from its
		// stable block ID. A block's widget definition is shared across all
		// instances of that block type, so without this each duplicate would
		// render with the SAME table FeId — producing duplicate DOM ids and a
		// shared URL filter namespace. The table resolver reads this override
		// (and the client echoes it back on data refreshes), so each instance
		// stays independently scoped without cloning the definition.
		blockReq := r.WithContext(context.WithValue(r.Context(), ctxkeys.FEIDOverride, b.ID))
		comp = spec.Render(w, blockReq)
	}
	layoutQuery := "&layout=" + strconv.Itoa(d.CurrentLayout)
	idQuery := "?id=" + url.QueryEscape(b.ID)
	return views.BlockData{
		ID:           b.ID,
		Cols:         clampCols(b.Cols, p.gridCols),
		Renderer:     comp,
		RemoveURL:    d.URLs.Remove + idQuery + layoutQuery,
		ResizeURL:    d.URLs.Resize + idQuery + layoutQuery,
		MoveURL:      d.URLs.Move + idQuery + layoutQuery,
		CanMoveLeft:  index > 0,
		CanMoveRight: index < count-1,
	}
}
