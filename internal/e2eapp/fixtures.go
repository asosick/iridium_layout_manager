package e2eapp

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a-h/templ"
	icons "github.com/iridiumgo/iridium-icons/icon/icons"
	"github.com/iridiumgo/iridium/core/collections"
	"github.com/iridiumgo/iridium/core/context/ctxForm"
	"github.com/iridiumgo/iridium/core/context/ctxTable"
	"github.com/iridiumgo/iridium/core/context/ctxWidget"
	"github.com/iridiumgo/iridium/core/context/ctxkeys"
	"github.com/iridiumgo/iridium/core/driver/slicedriver"
	"github.com/iridiumgo/iridium/core/field/components"
	"github.com/iridiumgo/iridium/core/form"
	"github.com/iridiumgo/iridium/core/table"
	tableAction "github.com/iridiumgo/iridium/core/table/action"
	tableComponents "github.com/iridiumgo/iridium/core/table/column/components"
	tableFilter "github.com/iridiumgo/iridium/core/table/filter"
	"github.com/iridiumgo/iridium/core/widget/chart"
	"github.com/iridiumgo/iridium/core/widget/stats"
	tablewidget "github.com/iridiumgo/iridium/core/widget/table"

	layoutmgr "github.com/asosick/iridium_layout_manager"
)

// SlowDelay is how long the "slow" fixture block takes to render.
const SlowDelay = 300 * time.Millisecond

// counters records how many times each fixture block was rendered (and how
// many times the table fixture queried its rows), so specs can count the
// work one mutation costs.
type counters struct {
	mu sync.Mutex
	m  map[string]*atomic.Int64
}

func newCounters() *counters { return &counters{m: map[string]*atomic.Int64{}} }

func (c *counters) inc(key string) {
	c.mu.Lock()
	n, ok := c.m[key]
	if !ok {
		n = &atomic.Int64{}
		c.m[key] = n
	}
	c.mu.Unlock()
	n.Add(1)
}

func (c *counters) snapshot() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int64, len(c.m))
	keys := make([]string, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = c.m[k].Load()
	}
	return out
}

func (c *counters) reset() {
	c.mu.Lock()
	c.m = map[string]*atomic.Int64{}
	c.mu.Unlock()
}

// blockID is the placed instance's ID: the plugin puts it on the request
// context of each block's render.
func blockID(r *http.Request) string {
	id, _ := r.Context().Value(ctxkeys.FEIDOverride).(string)
	return id
}

// fixtureBlock is a DynamicBlock with fixed geometry (set by fixtures.css)
// that prints its kind and instance ID, so order and content can be read
// from the DOM, and counts its renders.
func (a *App) fixtureBlock(kind, label string, delay time.Duration, body string) layoutmgr.BlockSpec {
	return layoutmgr.DynamicBlock(kind, label, func(_ http.ResponseWriter, r *http.Request) templ.Component {
		a.renders.inc(kind)
		if delay > 0 {
			time.Sleep(delay)
		}
		id := html.EscapeString(blockID(r))
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := fmt.Fprintf(w,
				`<div class="e2e-fixture e2e-%[1]s" data-e2e-kind="%[1]s" data-e2e-id="%[2]s"><strong>%[1]s</strong> <code>%[2]s</code>%[3]s</div>`,
				kind, id, body)
			return err
		})
	})
}

// controlsBody holds interactive content of the kind real widgets have: a
// text input, a natively draggable element and a sortable list of its own.
const controlsBody = `<input type="text" class="e2e-input" data-e2e-input aria-label="Fixture input" placeholder="type here">` +
	`<span class="e2e-draggable" draggable="true" data-e2e-draggable>drag me</span>` +
	`<ul class="e2e-sortlist" x-sort data-e2e-sortlist>` +
	`<li x-sort:item="'alpha'" data-e2e-item="alpha">alpha</li>` +
	`<li x-sort:item="'beta'" data-e2e-item="beta">beta</li>` +
	`<li x-sort:item="'gamma'" data-e2e-item="gamma">gamma</li>` +
	`</ul>`

// fixtureBlocks are the deterministic blocks:
//
//   - short (80px), tall (400px)
//   - grow: 80px, then 400px a moment after it is inserted (late content)
//   - slow: sleeps SlowDelay on every render
//   - controls: an input, a native draggable and a nested sortable list
func (a *App) fixtureBlocks() []layoutmgr.BlockSpec {
	return []layoutmgr.BlockSpec{
		a.fixtureBlock("short", "Short", 0, ""),
		a.fixtureBlock("tall", "Tall", 0, ""),
		a.fixtureBlock("grow", "Grow", 0, ""),
		a.fixtureBlock("slow", "Slow", SlowDelay, ""),
		a.fixtureBlock("controls", "Controls", 0, controlsBody),
	}
}

// --- real Iridium widgets ----------------------------------------------------

// Order is the table fixture's row type.
type Order struct {
	ID     string
	Number string
	Client string
	Status string
	Notes  string
}

// GetID implements core.Model.
func (o Order) GetID() string { return o.ID }

func seedOrders() []Order {
	return []Order{
		{ID: "1", Number: "#1001", Client: "Alder", Status: "Paid"},
		{ID: "2", Number: "#1002", Client: "Birch", Status: "Pending"},
		{ID: "3", Number: "#1003", Client: "Cedar", Status: "Paid"},
		{ID: "4", Number: "#1004", Client: "Dogwood", Status: "Refunded"},
		{ID: "5", Number: "#1005", Client: "Elm", Status: "Paid"},
		{ID: "6", Number: "#1006", Client: "Fir", Status: "Pending"},
		{ID: "7", Number: "#1007", Client: "Gum", Status: "Paid"},
	}
}

// widgetBlocks builds a fresh table, chart and stats widget. Each page needs
// its own instances: the plugin prefixes a widget's slug with the page's
// when it registers the widget's routes.
func (a *App) widgetBlocks() []layoutmgr.BlockSpec {
	orders := seedOrders()
	driver := slicedriver.NewSliceDriver(&orders, nil).
		WithGlobalQuery(func(_ func(i int) *Order, n int) []int {
			a.renders.inc("table-query")
			all := make([]int, n)
			for i := range all {
				all[i] = i
			}
			return all
		})
	orderTable := table.NewTable[Order](driver).
		Title("Orders").
		PerPage([]uint{3}).
		Columns(
			tableComponents.NewTextColumnResolvable[Order]("Number").Sortable().Searchable(),
			tableComponents.NewTextColumnResolvable[Order]("Client").Sortable().Searchable(),
			tableComponents.NewTextColumnResolvable[Order]("Status"),
		).
		Filters(
			tableFilter.NewSelectFilterResolvable[Order]("Status").
				Options([]collections.Pair[string, string]{
					{Key: "Paid", Value: "Paid"},
					{Key: "Pending", Value: "Pending"},
					{Key: "Refunded", Value: "Refunded"},
				}),
		).
		HeaderActions(
			tableAction.NewHeaderActionResolvable[Order]("add-note").
				Label("Add note").
				ModalForm(form.NewForm[Order](driver).
					Schema(components.NewInputResolvable[Order, string]("Notes")).
					Action(func(*ctxForm.FormSubmit[Order]) error { return nil })),
		).
		RowActions(
			tableAction.NewRowActionResolvable[Order]("inspect", nil).
				Label("Inspect").
				Callback(func(ctx *ctxTable.TableRowAction[Order]) error {
					ctx.GetWriter().Header().Set("X-E2E-Inspected", ctx.Model.ID)
					return nil
				}),
		)

	salesStats := stats.NewStatsWidgetResolvable("sales-stats").
		Stats(
			stats.NewStatResolvable("revenue").Title("Revenue").Description("12840").Icon(icons.DollarSign),
			stats.NewStatResolvable("orders").Title("Orders").Description("148").Icon(icons.ShoppingBag),
		)

	revenueChart := chart.NewChartWidgetResolvable[*ctxWidget.Widget]("revenue-chart").
		Label("Revenue").
		ChartType("bar").
		ChartData(map[string]any{
			"categories": []string{"Mon", "Tue", "Wed", "Thu", "Fri"},
			"series":     []map[string]any{{"name": "Revenue", "data": []int{14, 19, 17, 24, 22}}},
			"height":     160,
		})

	return []layoutmgr.BlockSpec{
		layoutmgr.WidgetBlock("table", "Orders table", tablewidget.NewTableWidgetResolvable[Order]("orders", orderTable)),
		layoutmgr.WidgetBlock("chart", "Revenue chart", revenueChart),
		layoutmgr.WidgetBlock("stats", "Sales stats", salesStats),
	}
}
