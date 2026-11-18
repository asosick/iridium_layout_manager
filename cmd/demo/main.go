// Package main runs a small, self-contained layout-manager showcase.
package main

import (
	"log"
	"net/http"
	"os"

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

	return layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
		Heading("Good morning, Alex").
		Description("Here is what is happening with your business this week.").
		Blocks(
			layoutmgr.WidgetBlock("sales", "Today at a glance", salesStats),
			layoutmgr.WidgetBlock("revenue", "Revenue overview", revenueChart),
			layoutmgr.WidgetBlock("orders", "Recent orders", ordersWidget),
		).
		GridColumns(3).
		ShowLockButton(true).
		Reorderable(true).
		Resizable(true).
		SkipAuth()
}
