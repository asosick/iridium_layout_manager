// Package main runs a small, self-contained layout-manager showcase.
package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/a-h/templ"

	"github.com/goiridium/iridium/config"
	"github.com/goiridium/iridium/core/context/ctxWidget"
	"github.com/goiridium/iridium/core/driver/slicedriver"
	"github.com/goiridium/iridium/core/iridium"
	"github.com/goiridium/iridium/core/panel"
	"github.com/goiridium/iridium/core/table"
	"github.com/goiridium/iridium/core/table/column/columns"
	"github.com/goiridium/iridium/core/widget/chart"
	"github.com/goiridium/iridium/core/widget/stats"
	tablewidget "github.com/goiridium/iridium/core/widget/tablewidget"
	"github.com/goiridium/iridium/network/middleware"
	icons "github.com/iridiumgo/iridium-icons/icon/icons"

	layoutmgr "github.com/asosick/iridium_layout_manager"
)

type order struct {
	ID     string
	Number string
	Client string
	Total  string
	Status string
}

func (o order) GetID() string { return o.ID }

type product struct {
	ID    string
	Name  string
	Sold  string
	Stock string
}

func (p product) GetID() string { return p.ID }

func main() {
	mux := http.NewServeMux()
	app := iridium.NewApp(demoConfig()).RegisterPanels(demoPanel(mux)).Initialize(mux)

	log.Printf("layout-manager demo: http://127.0.0.1:%s/admin/dashboard", app.Config.AppPort)
	if err := app.Serve(mux); err != nil {
		log.Fatal(err)
	}
}

func demoConfig() *config.IridiumConfig {
	cfg := config.New()
	cfg.AppName = "Iridium Layout Manager Demo"
	cfg.AppHost = "127.0.0.1"
	cfg.AppPort = "8088"
	if port := os.Getenv("DEMO_PORT"); port != "" {
		cfg.AppPort = port
	}
	cfg.AppKey = "ZGVtby1hcHAtc2VjcmV0LWtleS1jaGFuZ2UtbWUtbm93"
	cfg.AppEncryptionKey = "ZGVtby1lbmNyeXB0aW9uLWtleS1jaGFuZ2UtbWUtMzI="
	cfg.AppEnv = config.EnvDevelopment
	// Run the way production panels do: Alpine's CSP build under a strict
	// Content-Security-Policy (sent by SecurityHeadersCSP below).
	cfg.CSPMode = true
	return cfg
}

// demoPanel mounts the same middleware as a production panel (and Iridium's
// own E2E server). Spa wraps a full-page request in the panel shell, which
// is what loads Alpine, htmx and the plugin's script and stylesheet; without
// it /admin/dashboard answers a bare fragment and nothing is interactive.
func demoPanel(mux *http.ServeMux) *panel.Panel {
	return panel.New(mux).
		ID("layout-demo").
		Path("admin").
		NavStyle(panel.NavLeft).
		PanelItems(dashboardPage()).
		Middleware(
			middleware.SecurityHeadersCSP,
			middleware.CSRF,
		).
		PanelMiddleware(
			middleware.Compression,
			middleware.Spa,
			middleware.ErrorPages,
		)
}

func dashboardPage() *layoutmgr.LayoutManagerPage {
	var orders = []order{
		{ID: "1", Number: "#1048", Client: "Northstar Coffee", Total: "$2,480", Status: "Paid"},
		{ID: "2", Number: "#1047", Client: "Maple & Pine", Total: "$1,860", Status: "Paid"},
		{ID: "3", Number: "#1046", Client: "Fieldstone Studio", Total: "$980", Status: "Pending"},
		{ID: "4", Number: "#1045", Client: "Harbour Supply Co.", Total: "$3,240", Status: "Paid"},
		{ID: "5", Number: "#1044", Client: "Juniper House", Total: "$1,420", Status: "Refunded"},
	}
	orderDriver := slicedriver.New(&orders, nil)
	orderTable := table.New[order](orderDriver).
		Title("Recent orders").
		TitleDescription("The latest activity across your storefront").
		PerPage(5).
		Columns(
			columns.NewText[order]("Number").Sortable(),
			columns.NewText[order]("Client").Sortable(),
			columns.NewText[order]("Total").Sortable(),
			columns.NewText[order]("Status"),
		)

	salesStats := stats.New("sales-stats").
		Stats(
			stats.NewStat("revenue").Title("Revenue").Description("$12,840").Icon(icons.DollarSign),
			stats.NewStat("orders").Title("Orders").Description("148").Icon(icons.ShoppingBag),
			stats.NewStat("customers").Title("Customers").Description("2,840").Icon(icons.Users),
		)

	revenueChart := chart.New[*ctxWidget.Widget]("revenue-chart").
		Label("Revenue overview").
		ChartType("area").
		ChartData(map[string]any{
			"categories": []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
			"series":     []map[string]any{{"name": "Revenue", "data": []int{1480, 1920, 1760, 2480, 2240, 3160, 2840}}},
		})

	ordersWidget := tablewidget.New[order]("recent-orders", orderTable)

	channelChart := chart.New[*ctxWidget.Widget]("channel-chart").
		Label("Orders by channel").
		ChartType("bar").
		ChartData(map[string]any{
			"categories": []string{"Web", "Store", "Phone", "Partners"},
			"series":     []map[string]any{{"name": "Orders", "data": []int{86, 41, 12, 9}}},
			"height":     220,
		})

	trafficChart := chart.New[*ctxWidget.Widget]("traffic-chart").
		Label("Traffic sources").
		ChartType("donut").
		ChartData(map[string]any{
			"categories": []string{"Search", "Social", "Direct", "Email"},
			"series":     []int{44, 23, 21, 12},
			"height":     240,
		})

	conversionChart := chart.New[*ctxWidget.Widget]("conversion-chart").
		Label("Conversion rate").
		ChartType("line").
		ChartData(map[string]any{
			"categories": []string{"W1", "W2", "W3", "W4", "W5", "W6"},
			"series":     []map[string]any{{"name": "Conversion %", "data": []float64{2.1, 2.4, 2.2, 2.9, 3.1, 3.4}}},
			"height":     200,
		})

	customerStats := stats.New("customer-stats").
		Stats(
			stats.NewStat("new").Title("New customers").Description("214").Icon(icons.UserPlus),
			stats.NewStat("returning").Title("Returning").Description("61%").Icon(icons.Repeat),
		)

	var products = []product{
		{ID: "1", Name: "Cold brew kit", Sold: "412", Stock: "88"},
		{ID: "2", Name: "Ceramic pour-over", Sold: "296", Stock: "14"},
		{ID: "3", Name: "Oat milk (6 pack)", Sold: "254", Stock: "120"},
		{ID: "4", Name: "Travel mug", Sold: "187", Stock: "42"},
	}
	productTable := table.New[product](slicedriver.New(&products, nil)).
		Title("Top products").
		PerPage(4).
		Columns(
			columns.NewText[product]("Name").Sortable(),
			columns.NewText[product]("Sold").Sortable(),
			columns.NewText[product]("Stock"),
		)
	productsWidget := tablewidget.New[product]("top-products", productTable)

	return layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
		Heading("Good morning, Alex").
		Description("Here is what is happening with your business this week.").
		Blocks(
			layoutmgr.WidgetBlock("sales", "Today at a glance", salesStats),
			layoutmgr.WidgetBlock("revenue", "Revenue overview", revenueChart),
			layoutmgr.WidgetBlock("orders", "Recent orders", ordersWidget),
			layoutmgr.WidgetBlock("channels", "Orders by channel", channelChart),
			layoutmgr.WidgetBlock("traffic", "Traffic sources", trafficChart),
			layoutmgr.WidgetBlock("conversion", "Conversion rate", conversionChart),
			layoutmgr.WidgetBlock("customers", "Customers", customerStats),
			layoutmgr.WidgetBlock("products", "Top products", productsWidget),
			layoutmgr.StaticBlock("welcome", "Welcome note", welcomeNote()),
			layoutmgr.DynamicBlock("activity", "Live activity", liveActivity),
		).
		GridColumns(3).
		Settings().
		ShowLockButton(true).
		Reorderable(true).
		Resizable(true).
		SkipAuth()
}

// welcomeNote is a plain templ component: any markup can be a block.
func welcomeNote() templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<div style="padding:1rem 1.125rem;border:1px solid var(--color-border);border-radius:var(--radius,.5rem);background:var(--color-card,var(--color-background))">`+
			`<p style="margin:0;font-weight:600;font-size:1rem">Welcome back 👋</p>`+
			`<p style="margin:.375rem 0 0;color:var(--color-muted-foreground);font-size:.875rem;line-height:1.5">`+
			`Three orders are waiting to ship and the spring catalogue goes live on Friday. `+
			`Arrange this page however suits you: it is yours.</p></div>`)
		return err
	})
}

// liveActivity is a DynamicBlock: it renders per request. In the block
// gallery (layoutmgr.IsPreview) it shows a fixed sample instead of the live
// feed, as a camera or a slow query would show a still.
func liveActivity(_ http.ResponseWriter, r *http.Request) templ.Component {
	events := []string{"Northstar Coffee paid #1048", "Maple & Pine opened an invoice", "Juniper House asked for a refund"}
	stamp := time.Now().Format("15:04:05")
	badge := `<span style="display:inline-flex;align-items:center;gap:.375rem;font-size:.75rem;color:var(--color-destructive)">` +
		`<span style="width:.5rem;height:.5rem;border-radius:9999px;background:var(--color-destructive)"></span>Live · ` + stamp + `</span>`
	if layoutmgr.IsPreview(r) {
		badge = `<span style="font-size:.75rem;color:var(--color-muted-foreground)">Sample</span>`
	}
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		var items string
		for _, e := range events {
			items += fmt.Sprintf(`<li style="padding:.5rem 0;border-top:1px solid var(--color-border);font-size:.875rem">%s</li>`, html.EscapeString(e))
		}
		_, err := fmt.Fprintf(w, `<div style="padding:1rem 1.125rem;border:1px solid var(--color-border);border-radius:var(--radius,.5rem);background:var(--color-card,var(--color-background))">`+
			`<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:.5rem"><strong style="font-size:1rem">Activity</strong>%s</div>`+
			`<ul style="margin:0;padding:0;list-style:none">%s</ul></div>`, badge, items)
		return err
	})
}
