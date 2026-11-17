# Layout Manager: test report

Phase 3 of the testing plan (reviewed at `a156c1f`, Iridium v0.1.0-alpha.23).
Plugin behaviour is unchanged apart from the `cmd/demo` fix; every spec that
exposes a defect is committed as `test.fail()` (browser) or `knownDefect`
(Go) with its suspect number in the title, so both suites stay green and
each defect fails loudly once it is fixed.

## Running it

```bash
buildlock go test ./...
```

```bash
npm install
```

```bash
buildlock npx playwright test
```

The Go integration tests (`integration_test.go`, `defects_test.go`) drive the
real Iridium panel over HTTPS with a cookie jar: CSRF, Spa, error pages,
security headers, encrypted session cookies. The browser specs
(`test/e2e/*.spec.ts`) run against the same app (`internal/e2eapp`) in CSP
mode, and fail on any page error, console error or CSP violation.

## Verdicts

| # | Suspect | Verdict | Proved by |
|---|---|---|---|
| S1 | Screen order differs from stored order (dense packing) | **Confirmed** | `reorder-drag` "S1: with mixed heights and spans…", `reorder-buttons` "S1: with a two-column block, Move left…" |
| S2 | A second drag during an in-flight reorder is lost | **Confirmed** | `reorder-drag` "S2: a second drag while the first reorder is in flight is kept" |
| S3 | Lost updates between requests sent together | **Confirmed** | `defects_test.go` `TestS3_RequestsSentTogetherOverwriteEachOther`; `race` (three S3 specs); `reorder-drag` "S3/S5: a drag made while a resize is in flight…" |
| S4 | Cookie size ceiling | **Confirmed: 30 blocks** | `defects_test.go` `TestS4_LayoutsHitTheCookieCeiling`; `add-remove` "S4: a 31st block cannot be added…" |
| S5 | Every edit re-renders every block | **Confirmed** | `performance` "S5: one edit renders only what it changes"; `masonry` "S5: an edit to one block…"; `widgets` "S5: a table keeps its sort and page…"; `reorder-buttons` "S5: after a keyboard move…" |
| S6 | Sorting live in view mode / `x-sort:config` ignored | **Refuted** | `reorder-drag` both "S6" specs and the equal-height first/last specs |
| S7 | Reload loses the current page | **Confirmed** | `pages` "S7: a reload keeps the page you were on" |
| S8 | View mode shows the uncommitted draft | **Confirmed** | `persistence` "S8: after leaving without Done…" |
| S9 | LoadHook seeds once; Reset never re-reads it | **Confirmed** | `persistence` "S9: a browser that has edited keeps its own copy…" |
| S10 | Invalid `layout` parameter becomes 0 | **Confirmed** | `defects_test.go` `TestS10_AnInvalidLayoutIndexEditsPageOne` |
| S11 | Hotkeys: inputs, tab switching, cleanup | **Confirmed** (inputs, tab switching); **refuted** (cleanup) | `pages` both "S11" specs; `spa` both "S11" specs (pass) |
| S12 | Drag-resize limit differs from `+` limit | **Confirmed** | `resize` "S12: the drag handle can make a block as wide as the + button can" |
| S13 | No responsive behaviour | **Confirmed** | `responsive` "S13: on a phone-width screen the grid drops to one column" |
| S14 | Copies of a widget share DOM ids | **Confirmed** (ids); impact **refuted** | `widgets` "S14: copies of the same widget share no DOM ids"; both other "S14" specs pass |
| S15 | Inline `--lm-cols` style blocked by CSP | **Refuted** | `smoke` "S15: …applies under the CSP, before and after a swap" |
| S16 | `window.Alpine` / `window.htmx` missing | **Refuted** | `smoke` "S16: …"; page switching and reorder specs |

Found while testing (numbered on from the plan):

| # | Defect | Proved by |
|---|---|---|
| S17 | The layout cookies use gorilla's defaults (`Secure; SameSite=None`, 30 days, not HttpOnly) instead of Iridium's cookie options. Over plain HTTP the browser refuses them and no edit survives a reload: in Chromium on any non-loopback host, and in WebKit (Safari) even on `http://127.0.0.1`, so local development in Safari loses every edit. | `defects_test.go` `TestS17_LayoutsDoNotPersistOverPlainHTTP`; `persistence` both "S17: over plain HTTP…" specs |
| S18 | After any drag, every block control is dead in CSP mode until reload. The reorder response is fetched with `fetch()` and swapped with `htmx.swap`, which skips the hx-csp extension's response-nonce rewrite (it only rewrites htmx's own requests), so htmx strips `hx-*` from every button ("nonce mismatch (possible injection)"). | `reorder-drag` "S18: after a drag the block controls still work" |
| S19 | In Zen mode the fixed Exit Zen button sits under the panel's top bar and cannot be seen or clicked; only Escape leaves Zen. | `zen` "S19: the Exit Zen button can be seen and clicked" |
| S20 | A refused reorder (any non-2xx) is only logged to the console: the dragged order stays on screen though nothing was saved, and no error is shown. | `reorder-drag` "S20: a refused reorder is reported…" |

## Details

**S1.** Equal-height blocks reorder exactly in one and three columns: after
every drop, DOM, screen and stored order agree and survive a reload. Spans
break that. With `[tall, wide(2), a, b]`, dropping `b` on `wide` stores
`[tall, b, wide, a]`, but the screen reads `[tall, b, a, wide]` (`a`
back-fills the hole) and `b` is not under the release point. With
`[tall, a, wide(2), b]`, `b` is already drawn before `wide`, so Move left on
`b` changes the stored order and nothing moves on screen.

**S2.** With the first reorder held on the network, a second drag shows its
order, sends nothing, and is then reverted by the first response. One request
total.

**S3.** Each edit writes the whole layout back from the cookie its request
carried.
- Two adds sent together keep one block.
- A drag sent before a remove answered brings the removed block back.
- Two quick clicks on `+` grow a block by one.
- A remove during an in-flight reorder: the screen and the stored order end
  up different.
- Done pressed while a resize is in flight commits the pre-resize layout
  (committed 1 column, draft 2).
- A resize response that lands mid-drag replaces the grid under SortableJS:
  the drag is lost and an uncaught `TypeError` (`removeEventListener` of
  null) is thrown.

Done and Reset are disabled while a reorder is pending, and Done then commits
it (passing test). That is the only serialisation there is.

**S4.** 30 blocks fill the draft cookie (4040 bytes, about 134 encoded bytes
per block). The 31st add answers 500 (server log: `securecookie: the value is
too long: 4156`) and the user gets Iridium's generic error toast. The draft is
intact. The ceiling is per app, not per page: every layout-manager page and
all of their layouts share the one `layout-manager` cookie, so an empty page
of another dashboard then cannot take its first block either.

**S5.** On a 12-block page (one block renders in 300 ms, plus a real table,
chart and stats widget), measured one at a time:

| Edit | Response (ms) | Fixture blocks rendered | Table row queries |
|---|---:|---:|---:|
| resize | 334 | 9 | 1 |
| move | 331 | 9 | 1 |
| add | 356 | 10 | 1 |
| remove | 332 | 9 | 1 |
| reorder | 622 | **18** | **2** |
| switch to page 2 (1 block) | 75 | 1 | 0 |
| switch back | 323 | 9 | 1 |

Every edit renders every block of the layout (the 9 counted fixtures, plus
the widgets). A reorder renders everything twice: `persistAndRenderComponent`
builds the page data in `renderGrid`, then again in `renderGridOnly`.

What users see:
- During an unrelated resize, a block whose content loads late collapses from
  400 to 88 px and regrows (312 px drift), and an untouched block below it
  jumps 333 px.
- A table loses its sort and page.
- A keyboard user loses focus after each Move, so a second Enter does
  nothing.

The suspected one-frame flash from dropping `lm-masonry-ready` was not
observed: masonry runs in the next animation frame, before paint. The visible
movement comes from re-rendered content.

**S6.** Alpine's CSP build parses `x-sort:config`: the Sortable instance has
`handle: '.lm-block-handle'`. In view mode the handle is hidden and nothing
drags. In edit mode, dragging from content, an input, a native `draggable`
or an item of a block's own sortable list reorders nothing, and the nested
list still reorders itself. The resize guide child does not disturb
first-to-last or last-to-first drops.

**S11.** Ctrl/Cmd+N switches pages while typing in a text field, and the page
calls `preventDefault` on Ctrl/Cmd+1..N, the browser's tab shortcuts. Cleanup
works: after navigating away (SPA), the key is not swallowed and nothing is
sent; after three round trips, one press sends one request.

**S13.** A page loaded at 375 px keeps three columns of 101 px each. Touch
reordering by the handle on an emulated Pixel 7 works (passing test).

**S14.** Two copies of a chart share `chart-<slug>`. Two tables get unique
root ids from the per-instance override, but their filter, search and
pagination ids repeat: 11 duplicated ids across two copies each of table,
chart and stats. The tested behaviour does not suffer: two tables sort, page
and filter independently, and two charts and two stats widgets each render
in their own block.

**S15, S16.** Iridium's policy allows style attributes
(`style-src-attr 'unsafe-inline'`). The grid shows 3, 1 and 6 columns on
load and after an htmx swap with no violation. `window.Alpine`, `window.htmx`
(`ajax`, `swap`) and the security store's CSRF token are all present, and
page switching and reorder work end to end.

**Cross-browser.** The drag and resize specs also run in WebKit, over
HTTPS, because WebKit refuses the plugin's Secure cookies on plain HTTP
(S17). They behave exactly as in Chromium: the equal-height drags pass, and
S1, S2, S3/S5, S12, S18 and S20 reproduce. The Firefox project is configured
but could not run on this machine: Firefox's content-process sandbox cannot
start under the agent sandbox, so every launch times out.

Last full run: Chromium 59 tests (34 pass, 25 expected failures), WebKit 14
(8 pass, 6 expected failures), none unexpected. `go test ./...` passes,
including the `knownDefect` checks for S3, S4, S10 and S17.

## Suggested fix order

S18 first: one line of cause, every CSP-mode panel affected after the first
drag. Then S1 (honest order), S2/S3 (serialise edits, queue reorders), S5
(render the changed block only; the reorder double render is a quick win),
S4 and S17 together (server-side drafts, or at least Iridium's cookie
options), then the rest.

## Test suite changes

- `cmd/demo` mounts Spa, CSP-mode security headers, CSRF and error pages.
- `internal/e2eapp` is the shared test app; `test/e2e/server` serves it.
- Removed unit tests that grepped source text (`embed_test.go`) or rendered
  markup (`views/*_test.go`), and stubbed handler and state tests now covered
  through the real routes. `TestReorderUsesHTMXSwapLifecycle` required the
  very `htmx.swap` call behind S18.
