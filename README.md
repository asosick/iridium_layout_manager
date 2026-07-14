# Iridium Layout Manager

A standalone plugin for [Iridium](https://github.com/iridiumgo/iridium) that
lets your end-users arrange their own dashboard pages: pick blocks (Iridium
widgets or arbitrary `templ` components) from a dropdown, drop them onto the
page, drag-to-reorder, resize their column span, and commit the result. Layouts
persist per-user (cookie/session by default; pluggable for DB-backed storage).

Ported from the [FilamentPHP Filament Layout Manager](https://github.com/asosick/filament-layout-manager)
plugin to Iridium's HTMX + Alpine + templ stack.

Drag-to-reorder is powered by the Alpine Sort plugin that Iridium already bundles.

This plugin can serve as a starting point for understanding how to build your own Iridium plugin.
The beauty of Go is its duck typing lets you mix and match your custom code into Iridium fairly easily.

## Install

```bash
go get github.com/asosick/iridium_layout_manager@latest
```

## Quick start

```go
package main

import (
    "github.com/iridiumgo/iridium/core/panel"
    "github.com/iridiumgo/iridium/core/widget/stats"

    layoutmgr "github.com/asosick/iridium_layout_manager"
    "myapp/dashboard/components" // your own templ components
)

func main() {
    salesStats := stats.NewStatsWidgetResolvable("sales-stats").
        SetName("Sales Today")
        // ... configure stats ...

    dashboard := layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
        Blocks(
            layoutmgr.StaticBlock("welcome", "Welcome banner", components.Welcome()),
            layoutmgr.DynamicBlock("greeting", "Personal greeting",
                func(w http.ResponseWriter, r *http.Request) templ.Component {
                    return components.Hello(r) // can read the current user
                },
            ),
            layoutmgr.WidgetBlock("salesStats", "Sales stats", salesStats),
        ).
        GridColumns(3)

    p := panel.NewPanel("/admin").Pages(dashboard)
    // ... mount the panel as usual ...
}
```

That's it. Visit the page, click **🔒 Edit**, pick a block from the dropdown,
hit **+ Add** — your block appears on the grid. Drag the `⠿` handle or use the
left/right buttons to reorder. Resize with the corner handle or the size
buttons, and use `×` to remove. **Done** commits and locks the arrangement;
**Reset** restores the last committed arrangement.

Widgets are packed with a masonry-style grid, so a tall widget does not force
unrelated shorter widgets to leave an empty row beneath them.

## Block kinds

| Adapter | Use when |
|---|---|
| `StaticBlock(key, label, c)` | Tile is a fixed `templ.Component` (no per-request data). |
| `DynamicBlock(key, label, fn)` | Tile needs the `(w, r)` (DB queries, current user, etc.). |
| `WidgetBlock(key, label, w)` | Tile is an existing Iridium widget (chart, stats, form, table). |

You can mix them freely in a single `Blocks(...)` call.

## Configuration

```go
layoutmgr.NewLayoutManagerPage("Dashboard", "dashboard").
    Blocks(...).
    GridColumns(3).            // 1..N column grid
    LayoutCount(3).            // number of pages the user can flip between (default 3)
    Heading("My Dashboard").   // override the H1
    ShowLockButton(true).      // hide to keep the page permanently editable
    Reorderable(true).         // enable drag-to-reorder
    Resizable(true).           // enable resize controls
    SaveHook(myDBSave).        // optional — persist to DB when Done is clicked
    LoadHook(myDBLoad)         // optional — seed a new user session from DB
```

### Multiple pages

Each user gets `LayoutCount` separate pages (default 3) to arrange independently.
Numbered buttons at the top switch between them, and `cmd/ctrl + 1..9` jump
straight to a page. In edit mode every page number is shown so you can populate
empty ones; in view mode only pages that actually contain blocks show their
number (and the strip hides entirely when only one page is in use). All pages
persist together in a single `LayoutState`.

### Persistence

By default the plugin stores each user's layout in Iridium's gorilla session
(under a key namespaced by the page slug, separate from the auth cookie so
clearing one doesn't nuke the other). Zero config required.

To swap in your own storage, provide both hooks:

```go
type LoadHook func(r *http.Request) (LayoutState, error)
type SaveHook func(r *http.Request, state LayoutState) error
```

`LayoutState` is JSON-serialisable; just persist it as a blob keyed by user.

> **Note:** Working changes (add / remove / resize / reorder) are saved as a
> session draft so they survive page reloads. **Done** updates the committed
> session snapshot and calls `SaveHook`; **Reset** replaces the draft with that
> snapshot without calling the durable hook.

## How drag-to-reorder works

The plugin uses the Alpine Sort plugin that Iridium's core JS bundle already
ships with — no extra JS dependency. After a drop:

1. Alpine reorders the `<div data-lm-block>` elements in the DOM.
2. The layout manager catches SortableJS's completed `sort` event and reads
   the new order from the grid's `data-id` attributes.
3. It POSTs `{"order": [...]}` to the page's `/lm/reorder` endpoint.
4. The server reorders the saved state and returns the freshly-rendered
   grid; htmx swaps it back in.

The server's order is the source of truth, so the morph-swap also corrects
any DOM drift if a concurrent request lands.

## License
MIT
