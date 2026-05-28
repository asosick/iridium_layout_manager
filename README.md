# Iridium Layout Manager

A standalone plugin for [Iridium](https://github.com/iridiumgo/iridium) that
lets your end-users arrange their own dashboard pages: pick blocks (Iridium
widgets or arbitrary `templ` components) from a dropdown, drop them onto the
page, drag-to-reorder, resize their column span, and save the result. Layouts
persist per-user (cookie/session by default; pluggable for DB-backed storage).

Ported from the [FilamentPHP Filament Layout Manager](https://github.com/asosick/filament-layout-manager)
plugin to Iridium's HTMX + Alpine + templ stack. Drag-to-reorder is powered by
the Alpine Sort plugin that Iridium already bundles, so the plugin ships only
a tiny JS helper and a CSS file.

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
hit **+ Add** — your block appears on the grid. Drag the `⠿` handle to reorder,
use the `+`/`−`/`⇔` buttons to resize, and `×` to remove. Click **Save** to
commit the current arrangement (fires your `SaveHook`, or just persists the
session by default).

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
    Heading("My Dashboard").   // override the H1
    ShowLockButton(true).      // hide to lock the page in edit mode permanently
    Reorderable(true).         // enable drag-to-reorder
    Resizeable(true).          // enable +/-/full-width controls
    SaveHook(myDBSave).        // optional — persist to DB on Save click
    LoadHook(myDBLoad)         // optional — load from DB on each render
```

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

> **Note:** Working changes (add / remove / resize / reorder) are *always*
> committed to the session so the user's edits survive page reloads. Your
> `SaveHook` is only called when the user clicks the **Save** button — that's
> the "commit to permanent storage" event.

## How drag-to-reorder works

The plugin uses the Alpine Sort plugin that Iridium's core JS bundle already
ships with — no extra JS dependency. After a drop:

1. Alpine reorders the `<div data-lm-block>` elements in the DOM.
2. The `handleReorder` Alpine method (registered by the plugin) reads the
   new order from `data-id` attributes.
3. It POSTs `{"order": [...]}` to the page's `/lm/reorder` endpoint.
4. The server reorders the saved state and returns the freshly-rendered
   grid; htmx swaps it back in.

The server's order is the source of truth, so the morph-swap also corrects
any DOM drift if a concurrent request lands.

## Dev workflow

This module ships with a local `replace` directive pointing at a checkout of
iridium-core for development:

```go
// go.mod
replace github.com/iridiumgo/iridium => /Users/asosick/dev/iridium/iridium_forge/iridium-core
```

For production, comment that line out and run `go get -u github.com/iridiumgo/iridium`
to pull the published version.

## Status

v1 — single layout, session-backed by default, no per-block custom state.
Multi-layout tabs and per-block stores are deferred features from the
Filament original; both can land later without API breaks.

## License

MIT
