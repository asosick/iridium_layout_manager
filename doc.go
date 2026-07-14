// Package layoutmgr is an Iridium plugin that lets end-users arrange their own
// dashboards: pick blocks (widgets or arbitrary templ components) from a
// dropdown, drop them onto the page, drag to reorder, and resize their column
// span. Layouts are persisted per-user (session by default; pluggable via
// Save/Load hooks) so they survive across requests.
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
//	    GridColumns(3)
//
//	panel.Pages(layoutPage)
package layoutmgr
