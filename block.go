package layoutmgr

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/iridiumgo/iridium/core/widget"
)

// BlockSpec describes a block the user can add to the layout grid. The
// LayoutManagerPage is configured with a set of BlockSpecs and the user picks
// among them (by Label) in the "Add" dropdown.
//
// Implementations come in two flavours out of the box:
//
//   - Block(key, label, c) wraps a static templ.Component. Use this for arbitrary
//     dashboard tiles.
//   - WidgetBlock(key, label, w) adapts an iridium widget resolvable (the
//     same chart / stats / form / table widgets you'd use in any other page).
type BlockSpec interface {
	// Key uniquely identifies this block type. Stored in the layout state so
	// the server knows which BlockSpec to re-render each instance with.
	Key() string

	// Label is the human-readable name shown in the "Add" dropdown.
	Label() string

	// Render produces the templ.Component for ONE instance of this block, with
	// access to the current request (so the component can hit the database,
	// read the authed user, etc.).
	Render(w http.ResponseWriter, r *http.Request) templ.Component

	// RegisterRoutes is called once at boot, scoped under the page's mux, so
	// blocks that own HTTP routes (e.g. a widget that paginates its data) can
	// register them. Most blocks will leave this empty.
	RegisterRoutes(mux RouteRegistrar)
}

// RouteRegistrar is the subset of an HTTP mux a block needs to register routes.
// It exists so the plugin doesn't drag iridium's internal mux interface into
// every block author's import graph.
type RouteRegistrar interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// ---------------------------------------------------------------------------
// Adapters
// ---------------------------------------------------------------------------

// StaticBlock wraps a static templ.Component as a BlockSpec — the simplest
// possible adapter. Use this when your tile is purely presentational (no
// per-request data needed). For data-driven tiles, use DynamicBlock or
// WidgetBlock.
//
//	StaticBlock("welcome", "Welcome", components.Welcome())
func StaticBlock(key, label string, c templ.Component) BlockSpec {
	return &staticBlock{key: key, label: label, comp: c}
}

// DynamicBlock is like Block but renders via a per-request function — letting
// the block read the request (e.g. fetch the current user's data).
func DynamicBlock(key, label string, render func(w http.ResponseWriter, r *http.Request) templ.Component) BlockSpec {
	return &dynamicBlock{key: key, label: label, render: render}
}

// WidgetBlock adapts an iridium widget resolvable (chart / stats / form /
// table) into a BlockSpec. The widget's routes are registered under the page's
// mux automatically.
func WidgetBlock(key, label string, w widget.IWidgetResolvable) BlockSpec {
	return &widgetBlock{key: key, label: label, w: w}
}

// ---------------------------------------------------------------------------
// Implementations (unexported)
// ---------------------------------------------------------------------------

type staticBlock struct {
	key, label string
	comp       templ.Component
}

func (b *staticBlock) Key() string                                              { return b.key }
func (b *staticBlock) Label() string                                            { return b.label }
func (b *staticBlock) Render(_ http.ResponseWriter, _ *http.Request) templ.Component { return b.comp }
func (b *staticBlock) RegisterRoutes(RouteRegistrar)                            {}

type dynamicBlock struct {
	key, label string
	render     func(http.ResponseWriter, *http.Request) templ.Component
}

func (b *dynamicBlock) Key() string   { return b.key }
func (b *dynamicBlock) Label() string { return b.label }
func (b *dynamicBlock) Render(w http.ResponseWriter, r *http.Request) templ.Component {
	if b.render == nil {
		return templ.NopComponent
	}
	return b.render(w, r)
}
func (b *dynamicBlock) RegisterRoutes(RouteRegistrar) {}

type widgetBlock struct {
	key, label string
	w          widget.IWidgetResolvable
}

func (b *widgetBlock) Key() string   { return b.key }
func (b *widgetBlock) Label() string { return b.label }
func (b *widgetBlock) Render(w http.ResponseWriter, r *http.Request) templ.Component {
	return b.w.Component(w, r)
}
func (b *widgetBlock) RegisterRoutes(mux RouteRegistrar) {
	// The widget needs an iridium mux; if our caller hands us a compatible
	// adapter we forward to it. The plugin's layout.go is responsible for
	// wiring the widget into iridium's real mux at registration time — this
	// no-op here is the fallback for raw mux adapters that don't carry the
	// iridium type.
	_ = mux
}
