// Package main runs a small, self-contained layout-manager showcase.
package main

import (
	"log"
	"net/http"
	"os"

	icons "github.com/iridiumgo/iridium-icons/icon/icons"
	"github.com/iridiumgo/iridium/config"
	"github.com/iridiumgo/iridium/core/context/ctxWidget"
	"github.com/iridiumgo/iridium/core/driver/slicedriver"
	"github.com/iridiumgo/iridium/core/iridium"
	"github.com/iridiumgo/iridium/core/panel"
	"github.com/iridiumgo/iridium/core/table"
	"github.com/iridiumgo/iridium/core/table/column/components"
	"github.com/iridiumgo/iridium/core/widget/chart"
	"github.com/iridiumgo/iridium/core/widget/stats"
	tablewidget "github.com/iridiumgo/iridium/core/widget/table"
	"github.com/iridiumgo/iridium/network/middleware"

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
	app := iridium.NewIridiumApp(demoConfig()).RegisterPanels(demoPanel(mux)).Initialize(mux)

	log.Printf("layout-manager demo: http://127.0.0.1:%s/admin/dashboard", app.Config.AppPort)
	if err := app.Serve(mux); err != nil {
		log.Fatal(err)
	}
}

func demoConfig() *config.IridiumConfig {
	cfg := config.NewIridiumConfig()
	cfg.AppName = "Iridium Layout Manager Demo"
	cfg.AppHost = "127.0.0.1"
	cfg.AppPort = "8088"
	if port := os.Getenv("DEMO_PORT"); port != "" {
		cfg.AppPort = port
	}
	cfg.AppKey = "ZGVtby1hcHAtc2VjcmV0LWtleS1jaGFuZ2UtbWUtbm93"
	cfg.AppEncryptionKey = "ZGVtby1lbmNyeXB0aW9uLWtleS1jaGFuZ2UtbWUtMzI="
	cfg.AppEnv = "development"
	// Run the way production panels do: Alpine's CSP build under a strict
	// Content-Security-Policy (sent by SecurityHeadersMiddleware below).
	cfg.CSPMode = true
	return cfg
}

// demoPanel mounts the same middleware as a production panel (and Iridium's
// own E2E server). Spa wraps a full-page request in the panel shell, which
// is what loads Alpine, htmx and the plugin's script and stylesheet; without
// it /admin/dashboard answers a bare fragment and nothing is interactive.
func demoPanel(mux *http.ServeMux) *panel.Panel {
	return panel.NewPanel(mux).
		ID("layout-demo").
		Path("admin").
		NavStyle(panel.NavLeft).
		PanelItems(dashboardPage()).
		Middleware(
			middleware.SecurityHeadersMiddleware,
			middleware.CSRFMiddleware,
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
	orderDriver := slicedriver.NewSliceDriver(&orders, nil)
	orderTable := table.NewTable[order](orderDriver).
		Title("Recent orders").
		TitleDescription("The latest activity across your storefront").
		PerPage([]uint{5}).
		Columns(
			components.NewTextColumnResolvable[order]("Number").Sortable(),
			components.NewTextColumnResolvable[order]("Client").Sortable(),
			components.NewTextColumnResolvable[order]("Total").Sortable(),
			components.NewTextColumnResolvable[order]("Status"),
		)

	salesStats := stats.NewStatsWidgetResolvable("sales-stats").
		Stats(
			stats.NewStatResolvable("revenue").Title("Revenue").Description("$12,840").Icon(icons.DollarSign),
			stats.NewStatResolvable("orders").Title("Orders").Description("148").Icon(icons.ShoppingBag),
			stats.NewStatResolvable("customers").Title("Customers").Description("2,840").Icon(icons.Users),
		)

	revenueChart := chart.NewChartWidgetResolvable[*ctxWidget.Widget]("revenue-chart").
		Label("Revenue overview").
		ChartType("area").
		ChartData(map[string]any{
			"categories": []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
			"series":     []map[string]any{{"name": "Revenue", "data": []int{1480, 1920, 1760, 2480, 2240, 3160, 2840}}},
		})

	ordersWidget := tablewidget.NewTableWidgetResolvable[order]("recent-orders", orderTable)

	return layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
		Heading("Good morning, Alex").
		Description("Here is what is happening with your business this week.").
		Blocks(
			layoutmgr.WidgetBlock("sales", "Today at a glance", salesStats),
			layoutmgr.WidgetBlock("revenue", "Revenue overview", revenueChart),
			layoutmgr.WidgetBlock("orders", "Recent orders", ordersWidget),
		).
		GridColumns(3).
		LayoutCount(2).
		ShowLockButton(true).
		Reorderable(true).
		Resizable(true).
		SkipAuth()
}
