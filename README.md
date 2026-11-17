# Iridium Layout Manager

A standalone plugin for [Iridium](https://github.com/iridiumgo/iridium) that
lets your end-users arrange their own dashboard pages: pick blocks (Iridium
widgets or arbitrary `templ` components) from a dropdown, drop them onto the
page, drag-to-reorder, resize their column span, and commit the result. Layouts
are kept on the server per user, with hooks to store committed layouts in your
own database.

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
**Reset** returns to the last committed arrangement and keeps editing;
**Cancel** does the same and locks.

Edits are a draft until Done: they survive a reload and come back when the
user edits again, while view mode always shows the committed arrangement.

Blocks flow in order, left to right and top to bottom, each spanning as many
small rows as its content needs (masonry), so a tall widget does not force
unrelated shorter widgets to leave an empty row beneath them, and the order on
screen is the order stored. Under 32rem of width the grid becomes one column.

## Block kinds

| Adapter | Use when |
|---|---|
| `StaticBlock(key, label, c)` | Tile is a fixed `templ.Component` (no per-request data). |
| `DynamicBlock(key, label, fn)` | Tile needs the `(w, r)` (DB queries, current user, etc.). |
| `WidgetBlock(key, label, w)` | Tile is an existing Iridium widget (chart, stats, form, table). |

You can mix them freely in a single `Blocks(...)` call.

### Blocks that come and go

When the blocks on offer change while the app runs (one per camera, channel
or database record), give the page a `BlockSource` as well. It is asked on
every request, and its blocks are offered after the static ones:

```go
layoutmgr.NewLayoutManagerPage("Wall", "wall").
    BlockSource(func(r *http.Request) []layoutmgr.BlockSpec {
        var out []layoutmgr.BlockSpec
        for _, cam := range cameras.All() {
            out = append(out, layoutmgr.DynamicBlock("cam:"+cam.ID, cam.Name, cameraTile(cam)))
        }
        return out
    })
```

Source blocks register no routes, so keep a `WidgetBlock` whose widget has
routes in `Blocks`. A placed block whose key the source stops returning
renders as a missing block. Each placed instance's render request carries its
block ID (`ctxkeys.FEIDOverride`), so a block can keep settings per instance.

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
    LoadHook(myDBLoad)         // optional — read the committed layout from DB
```

### Multiple pages

Each user gets `LayoutCount` separate pages (default 3) to arrange independently.
Numbered buttons at the top switch between them, and the keys `1`..`9` jump
straight to a page (not while typing in a field, and not with Ctrl/Cmd, which
the browser uses to switch tabs). The page shown is kept in the address
(`?layout=N`), so a reload stays on it. In edit mode every page number is shown
so you can populate empty ones; in view mode only pages that actually contain
blocks show their number (and the strip hides entirely when only one page is in
use). All pages persist together in a single `LayoutState`.

### Persistence

The plugin keeps each user's layouts in the server's memory, per page: the
working draft that edits change, and the arrangement Done committed. Signed-in
users are keyed by their user ID, so their layouts follow them from browser to
browser; visitors who are not signed in get a key cookie (with Iridium's cookie
options). There is no size limit, and one user's edits apply one after another,
so edits sent at once all land.

Memory does not survive a restart, and is not shared between instances. To keep
committed layouts, provide both hooks:

```go
type LoadHook func(r *http.Request) (LayoutState, error)
type SaveHook func(r *http.Request, state LayoutState) error
```

`LayoutState` is JSON-serialisable; persist it as a blob keyed by user. With both
hooks, your storage is the committed layout: view mode, Reset and a new draft
read it through `LoadHook`, and Done writes it through `SaveHook` (nothing is
committed if the hook fails). Only unsaved drafts stay in memory, so with
several instances use sticky sessions, or accept that an unsaved draft is lost
when a user moves to another instance.

`Layouts` and `SetLayouts` read and replace a user's layouts from your own
handlers (an import, an administrator's reset).

## How editing works

The page is a `layout_manager` Alpine component and each block a
`layout_manager_block` component inside it (`static/layout_manager.js`).
Every request goes through one queue on the page, so each edit reaches the
server after the one before it has landed.

Edits change the page in place: remove, resize, move and reorder answer 204
(resize with the stored span in `X-Layout-Manager-Cols`), and Add answers the
one new block. The other blocks are never re-rendered, so widgets keep their
state (a table's sort and page, a chart) and focus stays where it was. Only
switching page, entering edit mode with unsaved changes, and Reset load a
whole grid.

Drag-to-reorder uses the Alpine Sort plugin that Iridium's core JS bundle
already ships with. After a drop the page posts the order the grid shows
(`order=id1&order=id2…`) to `/lm/reorder`; a drop made while one is on its way
is sent after it. If the server refuses a reorder, the page shows an error and
reloads the stored order.

## Development

`go run ./cmd/demo` serves a showcase panel at
http://127.0.0.1:8088/admin/dashboard (`DEMO_PORT` picks another port).

`go test ./...` runs the integration tests: real Iridium panel routes over
HTTPS, with CSRF and encrypted session cookies, through the test app in
`internal/e2eapp`.

The browser specs in `test/e2e` run against the same app in Iridium's CSP
mode:

```bash
npm install && npx playwright install chromium firefox webkit
```

```bash
npx playwright test
```

Known defects are kept as expected failures (`test.fail()`) with their number
in the title. A fix makes its marker fail, so remove the marker with the fix.
`TESTING_REPORT.md` lists them.

## License
MIT
