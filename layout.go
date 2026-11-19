package layoutmgr

import (
	"maps"
	"net/http"

	"github.com/a-h/templ"
	"github.com/goiridium/iridium/core/action/actions"
	"github.com/goiridium/iridium/core/action/traits"
	"github.com/goiridium/iridium/core/context/ctxPanel"
	"github.com/goiridium/iridium/core/enum"
	"github.com/goiridium/iridium/core/logger"
	"github.com/goiridium/iridium/core/page/pages"
	"github.com/goiridium/iridium/core/widget"
	"github.com/goiridium/iridium/network/wrapper"
	"github.com/iridiumgo/iridium-icons/icon"

	"github.com/asosick/iridium_layout_manager/views"
)

// LayoutManagerPage is an Iridium panel page that lets the end-user arrange
// dashboard blocks (widgets or arbitrary templ components). It's a drop-in
// page — register it with your panel like any other:
//
//	panel.PanelItems(layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
//	    Blocks(...).GridColumns(3))
//
// Layouts are kept on the server per user (see store.go), so they work with
// zero configuration. Pass SaveHook/LoadHook to keep committed layouts in
// your own database.
type LayoutManagerPage struct {
	*pages.CustomPanelPage

	blocks      []BlockSpec
	blockSource func(r *http.Request) []BlockSpec
	gridCols    int
	// layoutCount fixes the number of pages; 0 leaves it to the user, who
	// adds and removes pages.
	layoutCount  int
	showLockBtn  bool
	store        *layoutStore
	saveHook     SaveHook
	loadHook     LoadHook
	allowReorder bool
	allowResize  bool
	allowRename  bool
	zenEnabled   bool
	// settings adds the settings dialog, where the user chooses each page's
	// columns (up to maxCols, 0 for the default) and spacing.
	settings bool
	maxCols  int
	// toolbarRight puts the toolbar at the right of a header with no title.
	toolbarRight bool
}

// The most columns MaxColumns allows (the stylesheet has a block span for
// each up to it), and how many the settings dialog offers unless MaxColumns
// says otherwise.
const (
	maxGridColumns    = 12
	defaultMaxColumns = 6
)

// NewLayoutManagerPage constructs a layout-manager page rooted at the given
// slug. Until you add at least one Block(...) there is nothing to add — see
// Blocks().
//
// Defaults:
//   - GridColumns = 2
//   - Pages: one to start with, and the user adds more (see LayoutCount)
//   - RenamablePages = true
//   - ShowLockButton = true
//   - Reorderable = true
//   - Resizeable = true
//   - Persistence = per user, in the server's memory
func NewLayoutManagerPage(name, slug string) *LayoutManagerPage {
	cpp := pages.NewCustomPanelPage(name, slug)
	// Render the page name as the panel chrome's title (the same heading every
	// other panel page shows at the top). The plugin no longer draws its own
	// heading inside the content, so this is the single source for the title.
	cpp.Title(name)
	return &LayoutManagerPage{
		CustomPanelPage: cpp,
		gridCols:        2,
		showLockBtn:     true,
		allowReorder:    true,
		allowResize:     true,
		allowRename:     true,
		store:           newLayoutStore(),
	}
}

// --- builders ----------------------------------------------------------------

// Blocks registers the BlockSpecs the user can pick from in the block
// gallery. Order is preserved.
func (p *LayoutManagerPage) Blocks(blocks ...BlockSpec) *LayoutManagerPage {
	p.blocks = blocks
	return p
}

// BlockSource adds BlockSpecs the page asks for on every request, after
// those given to Blocks: for blocks that come and go while the app runs, such
// as one per record in a database. They are offered in the block gallery,
// added, rendered and removed like any other, but register no routes, so a
// WidgetBlock whose widget has routes belongs in Blocks. A placed block whose
// spec the source stops returning renders as a missing block.
func (p *LayoutManagerPage) BlockSource(fn func(r *http.Request) []BlockSpec) *LayoutManagerPage {
	p.blockSource = fn
	return p
}

// specs are the blocks on offer for r: Blocks, then BlockSource's.
func (p *LayoutManagerPage) specs(r *http.Request) []BlockSpec {
	if p.blockSource == nil {
		return p.blocks
	}
	return append(append([]BlockSpec{}, p.blocks...), p.blockSource(r)...)
}

// GridColumns sets the max columns in the underlying grid (default 2). Block
// instances can span 1..GridColumns columns each via resize. With Settings,
// it is the number of columns a page has until the user chooses another.
func (p *LayoutManagerPage) GridColumns(n int) *LayoutManagerPage {
	if n < 1 {
		n = 1
	}
	p.gridCols = n
	return p
}

// Settings adds a Settings button to edit mode. It opens a dialog in which
// the user chooses, for the page shown, its number of columns (1 to
// MaxColumns) and the space between its blocks. Like any other edit, the
// choice is kept by Done and undone by Reset or Cancel. A block as wide as
// the page stays as wide as the page when its number of columns changes; the
// others keep their span, up to the new number of columns.
//
// The choices are kept with the layout (Layout.Columns and Layout.Spacing),
// so a SaveHook stores them with the rest. Without Settings they are not
// offered, and stored ones are ignored: every page has GridColumns columns
// and the spacing its vertical padding gives.
func (p *LayoutManagerPage) Settings() *LayoutManagerPage {
	p.settings = true
	return p
}

// MaxColumns sets the most columns a user can choose in the settings dialog
// (default 6, or GridColumns when that is more; at most 12).
func (p *LayoutManagerPage) MaxColumns(n int) *LayoutManagerPage {
	p.maxCols = max(1, min(n, maxGridColumns))
	return p
}

// maxColumns is the most columns the settings dialog offers: never fewer
// than GridColumns, the number a page starts with.
func (p *LayoutManagerPage) maxColumns() int {
	n := p.maxCols
	if n == 0 {
		n = defaultMaxColumns
	}
	return max(n, p.gridCols)
}

// columnsOf is the number of columns page l has: the user's choice, while the
// settings are on, else GridColumns.
func (p *LayoutManagerPage) columnsOf(l Layout) int {
	if p.settings && l.Columns > 0 {
		return min(l.Columns, p.maxColumns())
	}
	return p.gridCols
}

// spacingOf is the spacing the user chose for page l, while the settings are
// on; SpacingDefault follows the page's vertical padding.
func (p *LayoutManagerPage) spacingOf(l Layout) Spacing {
	if p.settings && l.Spacing.valid() {
		return l.Spacing
	}
	return SpacingDefault
}

// AlignToolbarRight puts the toolbar (the page buttons, Edit and the rest) at
// the right of the page header when the page has no title or description,
// as it is beside one. By default it then sits at the left, as the actions
// of an Iridium page with no title do.
func (p *LayoutManagerPage) AlignToolbarRight() *LayoutManagerPage {
	p.toolbarRight = true
	return p
}

// LayoutCount fixes how many separate pages (layouts) every user has to
// arrange and flip between with the page buttons / 1..9 hotkeys. Without it,
// the number is the user's: they start with one page and add more with the
// + button, or remove them, in edit mode. Values < 1 are clamped to 1
// (single-page behaviour).
func (p *LayoutManagerPage) LayoutCount(n int) *LayoutManagerPage {
	if n < 1 {
		n = 1
	}
	p.layoutCount = n
	return p
}

// RenamablePages lets the user name the pages in edit mode (default true):
// the rename button or a double-click on a page's button. Unnamed pages show
// their number.
func (p *LayoutManagerPage) RenamablePages(enabled bool) *LayoutManagerPage {
	p.allowRename = enabled
	return p
}

// fixedPages reports whether LayoutCount fixed the number of pages.
func (p *LayoutManagerPage) fixedPages() bool {
	return p.layoutCount > 0
}

// pageCount is how many pages a (normalized) state has: LayoutCount, or the
// user's own number of pages.
func (p *LayoutManagerPage) pageCount(state LayoutState) int {
	if p.fixedPages() {
		return p.layoutCount
	}
	return max(1, len(state.Layouts))
}

// Zen adds a header action that hides the page header so the layout widgets
// can use the full content area. An Exit Zen button remains available while
// the header is hidden.
func (p *LayoutManagerPage) Zen() *LayoutManagerPage {
	p.zenEnabled = true
	return p
}

// Heading overrides the page title shown at the top of the page (defaults to
// the page name). Alias for Title — kept for back-compat.
func (p *LayoutManagerPage) Heading(h string) *LayoutManagerPage {
	p.CustomPanelPage.Title(h)
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

// Resizable enables the column-span controls in edit mode (default true).
func (p *LayoutManagerPage) Resizable(enabled bool) *LayoutManagerPage {
	p.allowResize = enabled
	return p
}

// Resizeable is retained for compatibility. Use Resizable in new code.
// Deprecated: use Resizable.
func (p *LayoutManagerPage) Resizeable(enabled bool) *LayoutManagerPage {
	return p.Resizable(enabled)
}

// SaveHook plugs in custom persistence (e.g. write to your database). The
// hook is called whenever the user clicks Done. Returning an error
// surfaces a notification to the user and commits nothing.
func (p *LayoutManagerPage) SaveHook(fn SaveHook) *LayoutManagerPage {
	if fn != nil {
		p.saveHook = fn
	}
	return p
}

// LoadHook plugs in custom retrieval (mirror of SaveHook). With a SaveHook
// too, it is the source of the committed layout: view mode, Reset and a new
// draft all read it, so every browser of a user sees what was last saved.
// Alone, it seeds users who have committed nothing on this server yet.
func (p *LayoutManagerPage) LoadHook(fn LoadHook) *LayoutManagerPage {
	if fn != nil {
		p.loadHook = fn
	}
	return p
}

// --- Navigation wrappers ----------------------------------------------------
//
// The embedded *CustomPanelPage already exposes NavIcon / NavLabel /
// NavGroup / NavOrder, but they return *CustomPanelPage which breaks fluent chaining
// from a LayoutManagerPage. These wrappers delegate to the embedded methods
// and return *LayoutManagerPage so you can keep the chain:
//
//	NewLayoutManagerPage("Dashboard", "dashboard").
//	    NavIcon(icons.LayoutDashboard).
//	    NavGroup("Overview").
//	    Blocks(...).
//	    GridColumns(3)

// NavLabel overrides the label shown in the sidebar (defaults to the page
// name).
func (p *LayoutManagerPage) NavLabel(label string) *LayoutManagerPage {
	p.CustomPanelPage.NavLabel(label)
	return p
}

// NavGroup places this page under a named group in the sidebar.
func (p *LayoutManagerPage) NavGroup(group string) *LayoutManagerPage {
	p.CustomPanelPage.NavGroup(group)
	return p
}

// NavIcon sets the sidebar icon for this page.
func (p *LayoutManagerPage) NavIcon(ic *icon.Icon) *LayoutManagerPage {
	p.CustomPanelPage.NavIcon(ic)
	return p
}

// NavOrder pins the page's sidebar position (lower = earlier).
func (p *LayoutManagerPage) NavOrder(order int) *LayoutManagerPage {
	p.CustomPanelPage.NavOrder(order)
	return p
}

// NavHidden hides this page from the sidebar (still reachable + shown in the
// sub-page tab strip); NavHidden(false) shows it again.
func (p *LayoutManagerPage) NavHidden(on ...bool) *LayoutManagerPage {
	p.CustomPanelPage.NavHidden(on...)
	return p
}

// TabHidden hides this page from the page-tab strip while leaving it in the
// sidebar; TabHidden(false) shows it again.
func (p *LayoutManagerPage) TabHidden(on ...bool) *LayoutManagerPage {
	p.CustomPanelPage.TabHidden(on...)
	return p
}

// --- Panel-page builder wrappers --------------------------------------------
//
// The embedded *CustomPanelPage exposes these, but they return
// *CustomPanelPage which breaks fluent chaining from a *LayoutManagerPage.
// Each wrapper delegates to the embedded method and returns *LayoutManagerPage
// so the whole configuration can be written as one chain. New methods added to
// CustomPanelPage should be mirrored here.

// Title sets the page title shown at the top of the page (defaults to name).
func (p *LayoutManagerPage) Title(value string) *LayoutManagerPage {
	p.CustomPanelPage.Title(value)
	return p
}

// TitleFn is a callback alias for Title.
func (p *LayoutManagerPage) TitleFn(fn func(ctx *ctxPanel.PanelPage) string) *LayoutManagerPage {
	p.CustomPanelPage.TitleFn(fn)
	return p
}

// Description sets the descriptive subtitle shown beneath the title.
func (p *LayoutManagerPage) Description(value string) *LayoutManagerPage {
	p.CustomPanelPage.Description(value)
	return p
}

// DescriptionFn is a callback alias for Description.
func (p *LayoutManagerPage) DescriptionFn(fn func(ctx *ctxPanel.PanelPage) string) *LayoutManagerPage {
	p.CustomPanelPage.DescriptionFn(fn)
	return p
}

// Attributes adds attributes to Iridium's native panel-page root.
func (p *LayoutManagerPage) Attributes(attributes templ.Attributes) *LayoutManagerPage {
	p.CustomPanelPage.Attributes(attributes)
	return p
}

// HeaderAttributes adds attributes to Iridium's native panel-page header.
func (p *LayoutManagerPage) HeaderAttributes(attributes templ.Attributes) *LayoutManagerPage {
	p.CustomPanelPage.HeaderAttributes(attributes)
	return p
}

// HeaderComponents renders components before the layout manager's native
// toolbar in Iridium's page action area.
func (p *LayoutManagerPage) HeaderComponents(components ...templ.Component) *LayoutManagerPage {
	p.CustomPanelPage.HeaderComponents(components...)
	return p
}

// ContentSize sets the page content width (see enum.PageSize).
func (p *LayoutManagerPage) ContentSize(value enum.PageSize) *LayoutManagerPage {
	p.CustomPanelPage.ContentSize(value)
	return p
}

// ContentSizeFn is a callback alias for ContentSize.
func (p *LayoutManagerPage) ContentSizeFn(fn func(ctx *ctxPanel.PanelPage) enum.PageSize) *LayoutManagerPage {
	p.CustomPanelPage.ContentSizeFn(fn)
	return p
}

// VerticalPadding sets the vertical spacing around and within the page (see
// enum.PageVerticalPadding). The space below the header and between blocks
// follows it.
func (p *LayoutManagerPage) VerticalPadding(value enum.PageVerticalPadding) *LayoutManagerPage {
	p.CustomPanelPage.VerticalPadding(value)
	return p
}

// VerticalPaddingFn is a callback alias for VerticalPadding.
func (p *LayoutManagerPage) VerticalPaddingFn(fn func(ctx *ctxPanel.PanelPage) enum.PageVerticalPadding) *LayoutManagerPage {
	p.CustomPanelPage.VerticalPaddingFn(fn)
	return p
}

// CompactVerticalPadding reduces the vertical spacing around and within the
// page, and the space between blocks with it.
func (p *LayoutManagerPage) CompactVerticalPadding() *LayoutManagerPage {
	p.CustomPanelPage.CompactVerticalPadding()
	return p
}

// NoVerticalPadding removes the vertical spacing around and within the page;
// blocks keep a hairline between them.
func (p *LayoutManagerPage) NoVerticalPadding() *LayoutManagerPage {
	p.CustomPanelPage.NoVerticalPadding()
	return p
}

// HeaderWidgets sets widgets rendered in the page header (above the grid).
func (p *LayoutManagerPage) HeaderWidgets(widgets ...widget.IWidgetResolvable) *LayoutManagerPage {
	p.CustomPanelPage.HeaderWidgets(widgets...)
	return p
}

// FooterWidgets sets widgets rendered in the page footer (below the grid).
func (p *LayoutManagerPage) FooterWidgets(widgets ...widget.IWidgetResolvable) *LayoutManagerPage {
	p.CustomPanelPage.FooterWidgets(widgets...)
	return p
}

// HeaderActions sets actions rendered on the right of the page header.
func (p *LayoutManagerPage) HeaderActions(acts ...actions.IActionResolvable[any]) *LayoutManagerPage {
	p.CustomPanelPage.HeaderActions(acts...)
	return p
}

// HeaderColumns sets the per-breakpoint grid column count for header widgets.
func (p *LayoutManagerPage) HeaderColumns(columns map[string]int) *LayoutManagerPage {
	p.CustomPanelPage.HeaderColumns(columns)
	return p
}

// HeaderColumnsFixed sets the same header column count at every breakpoint.
func (p *LayoutManagerPage) HeaderColumnsFixed(columns int) *LayoutManagerPage {
	p.CustomPanelPage.HeaderColumnsFixed(columns)
	return p
}

// FooterColumns sets the per-breakpoint grid column count for footer widgets.
func (p *LayoutManagerPage) FooterColumns(columns map[string]int) *LayoutManagerPage {
	p.CustomPanelPage.FooterColumns(columns)
	return p
}

// FooterColumnsFixed sets the same footer column count at every breakpoint.
func (p *LayoutManagerPage) FooterColumnsFixed(columns int) *LayoutManagerPage {
	p.CustomPanelPage.FooterColumnsFixed(columns)
	return p
}

// CanAccess installs an auth gate that runs before the page renders.
func (p *LayoutManagerPage) CanAccess(fn func(ctx *ctxPanel.PanelPage) bool) *LayoutManagerPage {
	p.CustomPanelPage.CanAccess(fn)
	return p
}

// SkipPanel skips the panel middleware defined on your panel.
func (p *LayoutManagerPage) SkipPanel() *LayoutManagerPage {
	p.CustomPanelPage.SkipPanel()
	return p
}

// SkipAuth skips the auth middleware defined on your panel.
func (p *LayoutManagerPage) SkipAuth() *LayoutManagerPage {
	p.CustomPanelPage.SkipAuth()
	return p
}

// --- IPage ------------------------------------------------------------------

// GetComponent is called per request to render the page. We construct a fresh
// per-request CustomPanelPage snapshot with our dynamically-built content
// (state-aware) and let iridium-core's chrome resolver wrap it in the panel.
// Each call gets its own snapshot so concurrent requests don't race on
// ContentObj.
//
// A page with a lock button opens in view mode and shows the committed
// layout; one without opens editable and shows the draft. The page shown is
// the one named by ?layout=N, else the first with blocks.
func (p *LayoutManagerPage) GetComponent(w http.ResponseWriter, r *http.Request) (templ.Component, error) {
	editing := !p.showLockBtn
	state, unsaved, err := p.shownState(w, r, editing)
	if err != nil {
		logger.Error("[layoutmgr] load layout: %v", err)
	}

	// Per-request snapshot — local copy, never shared. The native Iridium page
	// owns the Alpine controller and renders the layout toolbar directly in its
	// header action area, so every control remains inside the same lifecycle.
	d := p.buildInitialPageData(w, r, state, unsaved)
	cppCopy := p.cloneCustomPanelPage()
	cppCopy.ContentObj = views.LayoutManagerContent(d)
	cppCopy.AttributesObj = make(templ.Attributes, len(p.CustomPanelPage.AttributesObj)+len(views.PageAttributes(d)))
	maps.Copy(cppCopy.AttributesObj, p.CustomPanelPage.AttributesObj)
	maps.Copy(cppCopy.AttributesObj, views.PageAttributes(d))
	cppCopy.HeaderAttributesObj = make(templ.Attributes, len(p.CustomPanelPage.HeaderAttributesObj)+len(views.HeaderAttributes(d)))
	maps.Copy(cppCopy.HeaderAttributesObj, p.CustomPanelPage.HeaderAttributesObj)
	maps.Copy(cppCopy.HeaderAttributesObj, views.HeaderAttributes(d))
	cppCopy.HeaderComponentsArr = append(
		append([]templ.Component{}, p.CustomPanelPage.HeaderComponentsArr...),
		views.Toolbar(d),
	)

	return cppCopy.GetComponent(w, r)
}

func (p *LayoutManagerPage) cloneCustomPanelPage() pages.CustomPanelPage {
	page := *p.CustomPanelPage
	resolvable := *p.CustomPanelPage.PanelPageResolvable
	page.PanelPageResolvable = &resolvable
	return page
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

	// Plugin htmx endpoints live under the page slug, so they go on the
	// pageScoped (prefix) mux.
	p.registerLayoutRoutes(pageScoped)

	// MUST be registered AFTER widget actionables have been hooked into the
	// Carrier — RegisterCarrier walks the Carrier graph once and registers
	// every captured action route on the parent-scoped mux. This is what
	// makes table modals / search / filter / row-action routes light up.
	p.RegisterCarrier(scoped)
}

// --- layouts ------------------------------------------------------------------

// owner returns the request's user's layouts for this page.
func (p *LayoutManagerPage) owner(w http.ResponseWriter, r *http.Request) *userLayouts {
	return p.store.get(p.SlugStr + "|" + ownerKey(w, r))
}

// committedState is the layout Done last committed (u locked). With both
// hooks the application's storage holds it, so it is read from there. What
// the LoadHook returns is copied first: it may be the application's stored
// layout itself, and normalize and the edits change the state they get.
func (p *LayoutManagerPage) committedState(r *http.Request, u *userLayouts) (LayoutState, error) {
	if p.loadHook != nil && (u.committed == nil || p.saveHook != nil) {
		state, err := p.loadHook(r)
		if err != nil {
			return p.normalize(LayoutState{}), err
		}
		return p.normalize(clone(state)), nil
	}
	if u.committed != nil {
		return p.normalize(clone(*u.committed)), nil
	}
	return p.normalize(LayoutState{}), nil
}

// draftState is the layout being edited (u locked): the user's unsaved
// draft, else the committed layout.
func (p *LayoutManagerPage) draftState(r *http.Request, u *userLayouts) (LayoutState, error) {
	if u.draft != nil {
		return p.normalize(clone(*u.draft)), nil
	}
	return p.committedState(r, u)
}

// shownState is what the page shows: the draft while editing, else the
// committed layout. unsaved reports whether the draft differs from it.
func (p *LayoutManagerPage) shownState(w http.ResponseWriter, r *http.Request, editing bool) (LayoutState, bool, error) {
	u := p.owner(w, r)
	u.mu.Lock()
	defer u.mu.Unlock()
	unsaved := u.draft != nil
	if editing {
		state, err := p.draftState(r, u)
		return state, unsaved, err
	}
	state, err := p.committedState(r, u)
	return state, unsaved, err
}

// Layouts returns the request's user's layouts on this page: the draft edit
// mode shows (their unsaved edits, else the committed layout) and the
// committed layout view mode shows.
func (p *LayoutManagerPage) Layouts(w http.ResponseWriter, r *http.Request) (draft, committed LayoutState, err error) {
	u := p.owner(w, r)
	u.mu.Lock()
	defer u.mu.Unlock()
	if draft, err = p.draftState(r, u); err != nil {
		return draft, committed, err
	}
	committed, err = p.committedState(r, u)
	return draft, committed, err
}

// SetLayouts replaces the request's user's layouts on this page, as an
// import or an administrator would. A nil draft means no unsaved edits; a nil
// committed layout leaves the committed one as it is. It does not call the
// hooks.
func (p *LayoutManagerPage) SetLayouts(w http.ResponseWriter, r *http.Request, draft, committed *LayoutState) {
	u := p.owner(w, r)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.draft = nil
	if draft != nil {
		d := clone(*draft)
		u.draft = &d
	}
	if committed != nil {
		c := clone(*committed)
		u.committed = &c
	}
}

// normalize pads the layouts to LayoutCount (or one page, when the user
// adds them) and clamps each block's column span to its page's columns (in
// case the admin lowered the grid after layouts were saved).
func (p *LayoutManagerPage) normalize(state LayoutState) LayoutState {
	state.EnsureLayouts(max(1, p.layoutCount))
	for li := range state.Layouts {
		cols := p.columnsOf(state.Layouts[li])
		blocks := state.Layouts[li].Blocks
		for i := range blocks {
			blocks[i].Cols = clampCols(blocks[i].Cols, cols)
		}
	}
	return state
}

// blockByKey resolves a stored block Type to its BlockSpec, among Blocks and
// BlockSource's for r. Returns nil if the spec was removed since the layout
// was saved.
func (p *LayoutManagerPage) blockByKey(r *http.Request, key string) BlockSpec {
	for _, b := range p.specs(r) {
		if b.Key() == key {
			return b
		}
	}
	return nil
}
