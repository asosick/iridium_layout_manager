// Package main runs a small, self-contained layout-manager showcase.
package main

import (
	"log"
	"net/http"

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

	log.Println("layout-manager demo: http://127.0.0.1:8088/admin/dashboard")
	app.Serve(mux)
}

func demoConfig() *config.IridiumConfig {
	cfg := config.NewIridiumConfig()
	cfg.AppName = "Iridium Layout Manager Demo"
	cfg.AppHost = "127.0.0.1"
	cfg.AppPort = "8088"
	cfg.AppKey = "ZGVtby1hcHAtc2VjcmV0LWtleS1jaGFuZ2UtbWU="
	cfg.AppEncryptionKey = "ZGVtby1hcHAtZW5jcnlwdGlvbi1rZXktY2hhbmdlLW1l"
	cfg.AppEnv = "development"
	return cfg
}

func demoPanel(mux *http.ServeMux) *panel.Panel {
	return panel.NewPanel(mux).
		ID("layout-demo").
		Path("admin").
		NavStyle(panel.NavLeft).
		Pages(dashboardPage()).
		PanelMiddleware()
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
			components.NewTextColumnResolvable[order, slicedriver.ArraySearchQuery[order], slicedriver.ArraySortQuery[order]]("Number").Sortable(),
			components.NewTextColumnResolvable[order, slicedriver.ArraySearchQuery[order], slicedriver.ArraySortQuery[order]]("Client").Sortable(),
			components.NewTextColumnResolvable[order, slicedriver.ArraySearchQuery[order], slicedriver.ArraySortQuery[order]]("Total").Sortable(),
			components.NewTextColumnResolvable[order, slicedriver.ArraySearchQuery[order], slicedriver.ArraySortQuery[order]]("Status"),
		)

	salesStats := stats.NewStatsWidgetResolvable("sales-stats").
		Stats(
			stats.NewStatResolvable("revenue").Title("Revenue").Description("$12,840").Icon(icons.DollarSign),
			stats.NewStatResolvable("orders").Title("Orders").Description("148").Icon(icons.ShoppingBag),
			stats.NewStatResolvable("customers").Title("Customers").Description("2,840").Icon(icons.Users),
		)

	revenueChart := chart.NewResolvableChartWidget[*ctxWidget.Widget]("revenue-chart").
		WidgetName("Revenue overview").
		ChartType("area").
		ChartData(map[string]any{
			"categories": []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
			"series":     []int{1480, 1920, 1760, 2480, 2240, 3160, 2840},
		})

	ordersWidget := tablewidget.NewResolvableTableWidget[order]("recent-orders", orderTable)

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
