package layoutmgr

import (
	"net/http"
	"net/url"

	"github.com/a-h/templ"

	"github.com/asosick/iridium_layout_manager/views"
)

// renderPage builds the full PageData (heading, dropdown options, blocks
// with renderers + URLs) and returns the root LayoutManagerComponent.
// Called from GetComponent on every page render.
func (p *LayoutManagerPage) renderPage(w http.ResponseWriter, r *http.Request, state LayoutState) templ.Component {
	return views.LayoutManagerComponent(p.buildPageData(w, r, state))
}

// renderGrid returns just the grid component (htmx-swap target). Mutation
// handlers (add/remove/resize/reorder/save) return this instead of the full
// page.
func (p *LayoutManagerPage) renderGrid(w http.ResponseWriter, r *http.Request, state LayoutState) templ.Component {
	return views.Grid(p.buildPageData(w, r, state))
}

// buildPageData populates the view layer's PageData from the plugin's
// configuration and the current request's state. Per-block components are
// resolved here so any heavy lifting (DB queries inside a widget) happens
// once during this build, not at render time.
func (p *LayoutManagerPage) buildPageData(w http.ResponseWriter, r *http.Request, state LayoutState) *views.PageData {
	prefix := panelPathFromRequest(r) + p.SlugStr

	d := &views.PageData{
		Heading:        p.heading,
		GridColumns:    p.gridCols,
		ShowLockButton: p.showLockBtn,
		AllowReorder:   p.allowReorder,
		AllowResize:    p.allowResize,
		Request:        r,
		Writer:         w,
		URLs: views.PageURLs{
			Add:     prefix + routeAdd,
			Remove:  prefix + routeRemove,
			Resize:  prefix + routeResize,
			Reorder: prefix + routeReorder,
			Save:    prefix + routeSave,
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

	// Per-block render data. A block whose spec was removed since save renders
	// as a "broken" placeholder rather than crashing the whole page.
	d.Blocks = make([]views.BlockData, 0, len(state.Blocks))
	for _, b := range state.Blocks {
		spec := p.blockByKey(b.Type)
		var comp templ.Component
		if spec == nil {
			comp = views.MissingBlock(b.Type)
		} else {
			comp = spec.Render(w, r)
		}
		idQuery := "?id=" + url.QueryEscape(b.ID)
		d.Blocks = append(d.Blocks, views.BlockData{
			ID:              b.ID,
			Cols:            b.Cols,
			Renderer:        comp,
			RemoveURL:       d.URLs.Remove + idQuery,
			ResizeIncURL:    d.URLs.Resize + idQuery + "&delta=1",
			ResizeDecURL:    d.URLs.Resize + idQuery + "&delta=-1",
			ResizeToggleURL: d.URLs.Resize + idQuery + "&cols=0",
		})
	}

	// Ensure cols stays within bounds (defensive against an admin lowering
	// GridColumns after layouts were saved). Already done in currentState
	// but harmless to repeat.
	for i := range d.Blocks {
		d.Blocks[i].Cols = clampCols(d.Blocks[i].Cols, p.gridCols)
	}
	return d
}
