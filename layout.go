package layoutmgr

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/iridiumgo/iridium-icons/icon"
	"github.com/iridiumgo/iridium/core/action/traits"
	"github.com/iridiumgo/iridium/core/page/panel"
	"github.com/iridiumgo/iridium/network/wrapper"
)

// LayoutManagerPage is an Iridium panel page that lets the end-user arrange
// dashboard blocks (widgets or arbitrary templ components). It's a drop-in
// page — register it with your panel like any other:
//
//	panel.Pages(layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
//	    Blocks(...).GridColumns(3))
//
// Persistence defaults to the iridium session store, so layouts survive across
// requests with zero configuration. Pass SaveHook/LoadHook to plug your own
// database.
type LayoutManagerPage struct {
	*panel.CustomPanelPage

	blocks        []BlockSpec
	gridCols      int
	heading       string
	showLockBtn   bool
	saveHook      SaveHook
	loadHook      LoadHook
	allowReorder  bool
	allowResize   bool
	assetBasePath string // computed at register-time; relative to the panel
}

// NewLayoutManagerPage constructs a layout-manager page rooted at the given
// slug. Until you add at least one Block(...) the "Add" dropdown will be empty
// — see Blocks().
//
// Defaults:
//   - GridColumns = 2
//   - ShowLockButton = true
//   - Reorderable = true
//   - Resizeable = true
//   - Persistence = per-user session
func NewLayoutManagerPage(name, slug string) *LayoutManagerPage {
	cpp := panel.NewCustomPanelPage(name, slug)
	p := &LayoutManagerPage{
		CustomPanelPage: cpp,
		gridCols:        2,
		showLockBtn:     true,
		allowReorder:    true,
		allowResize:     true,
		heading:         name,
	}
	// Default persistence: per-page session key.
	sessionKey := "layout:" + cpp.SlugStr
	p.loadHook = sessionLoad(sessionKey)
	p.saveHook = sessionSave(sessionKey)
	return p
}

// --- builders ----------------------------------------------------------------

// Blocks registers the BlockSpecs the user can pick from in the "Add"
// dropdown. Order is preserved.
func (p *LayoutManagerPage) Blocks(blocks ...BlockSpec) *LayoutManagerPage {
	p.blocks = blocks
	return p
}

// GridColumns sets the max columns in the underlying grid (default 2). Block
// instances can span 1..GridColumns columns each via resize.
func (p *LayoutManagerPage) GridColumns(n int) *LayoutManagerPage {
	if n < 1 {
		n = 1
	}
	p.gridCols = n
	return p
}

// Heading overrides the H1 shown above the grid (defaults to the page name).
func (p *LayoutManagerPage) Heading(h string) *LayoutManagerPage {
	p.heading = h
	return p
}

// ShowLockButton toggles the lock/unlock control that flips between view and
// edit modes (default true). When hidden, the page is permanently in edit
// mode.
func (p *LayoutManagerPage) ShowLockButton(show bool) *LayoutManagerPage {
	p.showLockBtn = show
	return p
}

// Reorderable enables drag-to-reorder of blocks in edit mode (default true).
func (p *LayoutManagerPage) Reorderable(enabled bool) *LayoutManagerPage {
	p.allowReorder = enabled
	return p
}

// Resizeable enables +/- column-span controls in edit mode (default true).
func (p *LayoutManagerPage) Resizeable(enabled bool) *LayoutManagerPage {
	p.allowResize = enabled
	return p
}

// SaveHook plugs in custom persistence (e.g. write to your database). The
// hook is called whenever the user clicks the Save button. Returning an error
// surfaces a notification to the user.
func (p *LayoutManagerPage) SaveHook(fn SaveHook) *LayoutManagerPage {
	if fn != nil {
		p.saveHook = fn
	}
	return p
}

// LoadHook plugs in custom retrieval (mirror of SaveHook). Called on each
// page render and on each mutation to read the current state.
func (p *LayoutManagerPage) LoadHook(fn LoadHook) *LayoutManagerPage {
	if fn != nil {
		p.loadHook = fn
	}
	return p
}

// --- Navigation wrappers ----------------------------------------------------
//
// The embedded *CustomPanelPage already exposes NavigationIcon / Label /
// Group / Order, but they return *CustomPanelPage which breaks fluent chaining
// from a LayoutManagerPage. These wrappers delegate to the embedded methods
// and return *LayoutManagerPage so you can keep the chain:
//
//	NewLayoutManagerPage("Dashboard", "dashboard").
//	    NavigationIcon(icons.LayoutDashboard).
//	    NavigationGroup("Overview").
//	    Blocks(...).
//	    GridColumns(3)

// NavigationLabel overrides the label shown in the sidebar (defaults to the
// page name).
func (p *LayoutManagerPage) NavigationLabel(label string) *LayoutManagerPage {
	p.CustomPanelPage.NavigationLabel(label)
	return p
}

// NavigationGroup places this page under a named group in the sidebar.
func (p *LayoutManagerPage) NavigationGroup(group string) *LayoutManagerPage {
	p.CustomPanelPage.NavigationGroup(group)
	return p
}

// NavigationIcon sets the sidebar icon for this page.
func (p *LayoutManagerPage) NavigationIcon(ic *icon.Icon) *LayoutManagerPage {
	p.CustomPanelPage.NavigationIcon(ic)
	return p
}

// NavigationOrder pins the page's sidebar position (lower = earlier).
func (p *LayoutManagerPage) NavigationOrder(order int) *LayoutManagerPage {
	p.CustomPanelPage.NavigationOrder(order)
	return p
}

// --- IPage ------------------------------------------------------------------

// GetComponent is called per request to render the page. We construct a fresh
// per-request CustomPanelPage snapshot with our dynamically-built content
// (state-aware) and let iridium-core's chrome resolver wrap it in the panel.
// Each call gets its own snapshot so concurrent requests don't race on
// ContentObj.
func (p *LayoutManagerPage) GetComponent(w http.ResponseWriter, r *http.Request) (templ.Component, error) {
	// Stash w on the request context so default session-save hooks can emit
	// Set-Cookie headers. No-op for custom hooks that don't need it.
	r = attachWriter(r, w)

	state := p.currentState(r)

	// Per-request snapshot — local copy, never shared. The chrome resolver
	// reads ContentObj off this copy, leaving the page-level instance
	// untouched for the next request.
	cppCopy := *p.CustomPanelPage
	cppCopy.ContentObj = p.renderPage(w, r, state)

	return cppCopy.GetComponent(w, r)
}

// RegisterRoutes registers the main page route (delegated to BasePage) plus
// the plugin's own htmx endpoints (add/remove/resize/reorder/save) under the
// page's scoped mux. We override the embedded CustomPanelPage.RegisterRoutes
// so the page handler uses *our* GetComponent (the embedded type's
// RegisterRoutes captures its own method, missing our dynamic-content
// override).
//
// Widget routes are registered exactly the way iridium's PanelPageResolvable
// does it (see panel_page.go RegisterWidgets): each widget's slug is baked
// with the page slug, its Actionable trait is hung off the page's Carrier so
// nested action routes (table modals, search/filter endpoints, row actions)
// get registered, and routes go on the parent-scoped mux (NOT pre-prefixed
// with the page slug) since the slug is now part of the widget's identity.
// Skipping any one of those steps breaks modals / search / filters — which
// is why table widgets were previously broken.
func (p *LayoutManagerPage) RegisterRoutes(mux wrapper.IMux) {
	scoped := p.GetPageMux(mux)
	pageScoped := scoped
	if p.SlugStr != "" {
		pageScoped = wrapper.NewPrefixMux(scoped, p.SlugStr)
	}

	// Main GET — uses our override that injects per-request content.
	p.BasePage.RegisterRoutes(scoped, p.GetComponent)

	// Sub-page tabs from CustomPanelPage (if the consumer added any).
	for _, sub := range p.GetSubPages() {
		sub.RegisterRoutes(scoped)
	}

	// Wire each widget block exactly like PanelPageResolvable.RegisterWidgets:
	// nest the widget under the page slug, hook its actionable into the
	// Carrier (so nested action routes register), then register the widget's
	// own routes on the parent-scoped mux.
	for _, b := range p.blocks {
		wb, ok := b.(*widgetBlock)
		if !ok || wb.w == nil {
			continue
		}
		wb.w.SetSlug(p.SlugStr + wb.w.GetSlug())
		if aw, ok := wb.w.(interface{ GetActionable() *traits.Actionable }); ok {
			p.GetCarrier().PrefixedSubActionable(wb.w.GetSlug(), aw.GetActionable())
		}
		wb.w.RegisterRoutes(scoped)
	}

	// Plugin htmx endpoints + embedded static assets — these live under the
	// page slug, so they go on the pageScoped (prefix) mux.
	p.registerLayoutRoutes(pageScoped)
	p.registerAssetRoutes(pageScoped)

	// MUST be registered AFTER widget actionables have been hooked into the
	// Carrier — RegisterCarrier walks the Carrier graph once and registers
	// every captured action route on the parent-scoped mux. This is what
	// makes table modals / search / filter / row-action routes light up.
	p.RegisterCarrier(scoped)
}

// --- helpers ----------------------------------------------------------------

// currentState loads from the configured hook and normalises the result
// (clamping column spans to the current GridColumns setting, in case the
// admin lowered the grid after layouts were saved).
func (p *LayoutManagerPage) currentState(r *http.Request) LayoutState {
	state, err := p.loadHook(r)
	if err != nil {
		// Soft-fail to an empty state so the page still renders.
		state = LayoutState{}
	}
	for i := range state.Blocks {
		state.Blocks[i].Cols = clampCols(state.Blocks[i].Cols, p.gridCols)
	}
	return state
}

// blockByKey resolves a stored block Type to its registered BlockSpec.
// Returns nil if the spec was removed since the layout was saved.
func (p *LayoutManagerPage) blockByKey(key string) BlockSpec {
	for _, b := range p.blocks {
		if b.Key() == key {
			return b
		}
	}
	return nil
}
