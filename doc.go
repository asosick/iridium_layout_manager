// Package layoutmgr is an Iridium plugin that lets end-users arrange their own
// dashboards: pick blocks (widgets or arbitrary templ components) from a
// gallery of live previews, drag them onto the page, drag to reorder, and
// resize their column span. Layouts are kept on the server per user (in
// memory by default; commit them to your own storage with Save/Load hooks).
//
// This is a Go port of the FilamentPHP "Filament Layout Manager" plugin
// (https://github.com/asosick/filament-layout-manager), adapted to Iridium's
// HTMX + Alpine + templ stack. The Alpine sort plugin (already bundled by
// iridium-core) handles drag-to-reorder.
//
// Basic usage:
//
//	layoutPage := layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
//	    Blocks(
//	        layoutmgr.StaticBlock("welcome", "Welcome", welcomeBlock()),
//	        layoutmgr.WidgetBlock("salesChart", "Sales Chart", salesChartWidget),
//	    ).
//	    GridColumns(map[string]int{"xs": 1, "md": 2, "lg": 3})
//
//	panel.PanelItems(layoutPage)
package layoutmgr
