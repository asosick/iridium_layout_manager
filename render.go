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

// renderPage builds the full PageData (heading, dropdown options, blocks
// with renderers + URLs) and returns the root LayoutManagerComponent.
// Called from GetComponent on every page render.
func (p *LayoutManagerPage) renderPage(w http.ResponseWriter, r *http.Request, state LayoutState) templ.Component {
	// On a fresh page load, focus the first layout that has content so a user
	// who saved everything on (say) layout 2 doesn't land on an empty page 1.
	current := state.FirstUsedLayout(p.layoutCount)
	return views.LayoutManagerComponent(p.buildPageData(w, r, state, current))
}

// renderGrid returns just the grid component (htmx-swap target). Mutation
// handlers (add/remove/resize/reorder/save) return this instead of the full
// page.
func (p *LayoutManagerPage) renderGrid(w http.ResponseWriter, r *http.Request, state LayoutState, currentLayout int) templ.Component {
	return views.GridResponse(p.buildPageData(w, r, state, currentLayout))
}

func (p *LayoutManagerPage) renderGridOnly(w http.ResponseWriter, r *http.Request, state LayoutState, currentLayout int) templ.Component {
	return views.Grid(p.buildPageData(w, r, state, currentLayout))
}

// buildPageData populates the view layer's PageData from the plugin's
// configuration and the current request's state. Per-block components are
// resolved here so any heavy lifting (DB queries inside a widget) happens
// once during this build, not at render time.
func (p *LayoutManagerPage) buildPageData(w http.ResponseWriter, r *http.Request, state LayoutState, currentLayout int) *views.PageData {
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
			CSS:     p.assetURL(r, "layout_manager.css"),
			JS:      p.assetURL(r, "layout_manager.js"),
		},
	}

	// Dropdown options come straight from the registered specs.
	d.AddOptions = make([]views.BlockOption, 0, len(p.blocks))
	for _, b := range p.blocks {
		d.AddOptions = append(d.AddOptions, views.BlockOption{
			Key:   b.Key(),
			Label: b.Label(),
		})
	}

	// Per-block render data for the currently-selected layout only. A block
	// whose spec was removed since save renders as a "broken" placeholder
	// rather than crashing the whole page.
	//
	// Every per-block URL carries &layout=N so a mutation lands on the layout
	// the grid is actually showing (the grid is re-rendered per layout switch).
	layoutQuery := "&layout=" + strconv.Itoa(currentLayout)
	curBlocks := state.LayoutAt(currentLayout, p.layoutCount).Blocks
	d.Blocks = make([]views.BlockData, 0, len(curBlocks))
	for blockIndex, b := range curBlocks {
		spec := p.blockByKey(b.Type)
		var comp templ.Component
		if spec == nil {
			comp = views.MissingBlock(b.Type)
		} else {
			// Give every block instance a unique front-end id derived from its
			// stable block ID. A block's widget definition is shared across all
			// instances of that block type, so without this each duplicate would
			// render with the SAME table FeId — producing duplicate DOM ids and a
			// shared URL filter namespace, which is why changing a filter on one
			// duplicated table leaked into the others. The table resolver reads
			// this override (and the client echoes it back on data refreshes), so
			// each instance stays independently scoped without cloning the
			// definition. Harmless for non-table blocks.
			blockReq := r.WithContext(context.WithValue(r.Context(), ctxkeys.FEIDOverride, b.ID))
			comp = spec.Render(w, blockReq)
		}
		idQuery := "?id=" + url.QueryEscape(b.ID)
		d.Blocks = append(d.Blocks, views.BlockData{
			ID:              b.ID,
			Cols:            clampCols(b.Cols, p.gridCols),
			Renderer:        comp,
			RemoveURL:       d.URLs.Remove + idQuery + layoutQuery,
			ResizeIncURL:    d.URLs.Resize + idQuery + "&delta=1" + layoutQuery,
			ResizeDecURL:    d.URLs.Resize + idQuery + "&delta=-1" + layoutQuery,
			ResizeToggleURL: d.URLs.Resize + idQuery + "&cols=0" + layoutQuery,
			ResizeURL:       d.URLs.Resize + idQuery + layoutQuery,
			MoveLeftURL:     d.URLs.Move + idQuery + "&delta=-1" + layoutQuery,
			MoveRightURL:    d.URLs.Move + idQuery + "&delta=1" + layoutQuery,
			CanMoveLeft:     blockIndex > 0,
			CanMoveRight:    blockIndex < len(curBlocks)-1,
		})
	}
	// The reorder POST also needs to know which layout it's reordering.
	d.ReorderURL = d.URLs.Reorder + "?layout=" + strconv.Itoa(currentLayout)
	return d
}
