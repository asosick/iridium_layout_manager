// Package e2eapp builds the layout manager's end-to-end test application: a
// real Iridium panel, mounted with the same middleware a production panel
// uses (CSP mode, security headers, CSRF, Spa, error pages), with one
// LayoutManagerPage per configuration under test and fixture blocks of known
// geometry.
//
// It is shared by the Playwright server (test/e2e/server) and the Go
// integration tests, so both drive exactly the same routes. Iridium keeps its
// configuration, session store and asset registry in package globals, so New
// may be called once per process.
//
// The /e2e/* endpoints are test-only. They take no credentials, which is why
// the server binds to loopback only.
package e2eapp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing/fstest"

	"github.com/a-h/templ"
	iridiumconfig "github.com/goiridium/iridium/config"
	"github.com/goiridium/iridium/core/asset"
	"github.com/goiridium/iridium/core/auth/user"
	"github.com/goiridium/iridium/core/context/ctxkeys"
	"github.com/goiridium/iridium/core/iridium"
	"github.com/goiridium/iridium/core/page/pages"
	"github.com/goiridium/iridium/core/panel"
	"github.com/goiridium/iridium/network/middleware"

	layoutmgr "github.com/asosick/iridium_layout_manager"
)

// PanelPath is where the test panel is mounted.
const PanelPath = "/admin"

// Page slugs, one per configuration under test.
const (
	PageDefault   = "default"   // 3 columns, 3 layouts, lock button, every block
	PageSingle    = "single"    // one layout
	PageNarrow    = "narrow"    // 1 column
	PageWide      = "wide"      // 6 columns
	PageNoLock    = "nolock"    // no lock button: always editable
	PageNoReorder = "noreorder" // reordering disabled
	PageNoResize  = "noresize"  // resizing disabled
	PageZen       = "zen"       // Zen mode enabled
	PageHooks     = "hooks"     // SaveHook/LoadHook on an in-memory "database"
	PageSource    = "source"    // BlockSource whose keys a test can change
	PagePages     = "pages"     // the defaults: users add, remove and name pages
	PageCompact   = "compact"   // Iridium's compact vertical padding
	PageSettings  = "settings"  // the settings dialog: columns and spacing
	PageUntitled  = "untitled"  // no title: the toolbar at the left
	PageRight     = "right"     // no title, AlignToolbarRight
	PageElsewhere = "elsewhere" // a plain page to navigate away to
)

// LayoutPages are the slugs of every LayoutManagerPage.
var LayoutPages = []string{
	PageDefault, PageSingle, PageNarrow, PageWide, PageNoLock,
	PageNoReorder, PageNoResize, PageZen, PageHooks, PageSource,
	PagePages, PageCompact, PageSettings, PageUntitled, PageRight,
}

//go:embed static
var staticFS embed.FS

var created atomic.Bool

// App is the test application.
type App struct {
	Mux *http.ServeMux

	pages   map[string]*layoutmgr.LayoutManagerPage
	renders *counters
	hooks   *hookDB

	sourceMu   sync.Mutex
	sourceKeys []string
}

var defaultSourceKeys = []string{"cam-1", "cam-2"}

// New builds the application. It panics if called twice in one process.
func New() *App {
	if !created.CompareAndSwap(false, true) {
		panic("e2eapp.New called twice: Iridium's configuration is process-global")
	}
	a := &App{
		Mux:        http.NewServeMux(),
		pages:      make(map[string]*layoutmgr.LayoutManagerPage),
		renders:    newCounters(),
		hooks:      &hookDB{},
		sourceKeys: slices.Clone(defaultSourceKeys),
	}

	cfg := iridiumconfig.New()
	cfg.AppName = "Layout Manager E2E"
	// Test-only keys - never use in production.
	cfg.AppKey = "dGVzdC1hcHAta2V5LWZvci11bml0LXRlc3Rpbmctb25seS10ZXN0aW5n"
	cfg.AppEncryptionKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	cfg.AppEnv = iridiumconfig.EnvTest
	cfg.CSPMode = true
	// Errors only: the plugin logs a failed save at error level, which is
	// worth seeing next to a failing test; everything else is noise.
	cfg.InternalLogLevel = iridiumconfig.LogLevelError

	// Without a favicon the browser's /favicon.ico request logs a 404.
	asset.Favicon(fstest.MapFS{"favicon.svg": &fstest.MapFile{Data: []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7" fill="#64748b"/></svg>`,
	)}}, "favicon.svg")
	asset.Register(asset.CSS("e2e-fixtures", staticFS, "static/fixtures.css").Package("e2e"))

	iridium.NewApp(cfg).
		RegisterPanels(a.panel()).
		Initialize(a.Mux)

	a.registerTestRoutes()
	return a
}

func (a *App) panel() *panel.Panel {
	both := func() []layoutmgr.BlockSpec { return append(a.fixtureBlocks(), a.widgetBlocks()...) }
	simple := func() []layoutmgr.BlockSpec { return a.fixtureBlocks() }

	// Three pages each, as before pages could be added, except where a page
	// tests the number of pages itself.
	layoutPages := []*layoutmgr.LayoutManagerPage{
		layoutmgr.NewLayoutManagerPage("Default", PageDefault).Blocks(both()...).GridColumns(3).LayoutCount(3),
		layoutmgr.NewLayoutManagerPage("Single", PageSingle).Blocks(simple()...).GridColumns(3).LayoutCount(1),
		layoutmgr.NewLayoutManagerPage("Narrow", PageNarrow).Blocks(simple()...).GridColumns(1).LayoutCount(3),
		layoutmgr.NewLayoutManagerPage("Wide", PageWide).Blocks(simple()...).GridColumns(6).LayoutCount(3),
		layoutmgr.NewLayoutManagerPage("No lock", PageNoLock).Blocks(simple()...).GridColumns(3).LayoutCount(3).ShowLockButton(false),
		layoutmgr.NewLayoutManagerPage("No reorder", PageNoReorder).Blocks(simple()...).GridColumns(3).LayoutCount(3).Reorderable(false),
		layoutmgr.NewLayoutManagerPage("No resize", PageNoResize).Blocks(simple()...).GridColumns(3).LayoutCount(3).Resizable(false),
		layoutmgr.NewLayoutManagerPage("Zen", PageZen).Blocks(simple()...).GridColumns(3).LayoutCount(3).Zen(),
		layoutmgr.NewLayoutManagerPage("Hooks", PageHooks).Blocks(simple()...).GridColumns(3).LayoutCount(3).
			SaveHook(a.hooks.save).LoadHook(a.hooks.load),
		layoutmgr.NewLayoutManagerPage("Source", PageSource).
			Blocks(a.fixtureBlock("note", "Note", 0, "")).
			GridColumns(3).
			LayoutCount(3).
			BlockSource(a.sourceBlocks),
		layoutmgr.NewLayoutManagerPage("Pages", PagePages).Blocks(simple()...).GridColumns(3),
		layoutmgr.NewLayoutManagerPage("Compact", PageCompact).Blocks(simple()...).GridColumns(3).LayoutCount(3).
			CompactVerticalPadding(),
		layoutmgr.NewLayoutManagerPage("Settings", PageSettings).Blocks(simple()...).GridColumns(3).LayoutCount(3).
			Settings(),
		layoutmgr.NewLayoutManagerPage("Untitled", PageUntitled).Blocks(simple()...).GridColumns(3).Title(""),
		layoutmgr.NewLayoutManagerPage("Right", PageRight).Blocks(simple()...).GridColumns(3).Title("").
			AlignToolbarRight(),
	}
	items := make([]pages.IPanelItem, 0, len(layoutPages)+1)
	for _, p := range layoutPages {
		a.pages[strings.TrimPrefix(p.SlugStr, "/")] = p
		items = append(items, p)
	}
	items = append(items, pages.NewCustomPanelPage("Elsewhere", PageElsewhere).
		Content(templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, `<p data-e2e-elsewhere>Nothing to arrange here.</p>`)
			return err
		})))

	return panel.New(a.Mux).
		ID("e2e").
		Path(strings.TrimPrefix(PanelPath, "/")).
		NavStyle(panel.NavLeft).
		PanelItems(items...).
		Middleware(
			middleware.SecurityHeadersCSP,
			middleware.CSRF,
			signedIn,
		).
		PanelMiddleware(
			middleware.Compression,
			middleware.Spa,
			middleware.ErrorPages,
		)
}

// sourceBlocks is the source page's BlockSource: one block per current key.
func (a *App) sourceBlocks(*http.Request) []layoutmgr.BlockSpec {
	a.sourceMu.Lock()
	keys := slices.Clone(a.sourceKeys)
	a.sourceMu.Unlock()
	out := make([]layoutmgr.BlockSpec, 0, len(keys))
	for _, key := range keys {
		out = append(out, a.fixtureBlock(key, strings.ToUpper(key), 0, ""))
	}
	return out
}

// --- hooks page "database" ---------------------------------------------------

type hookDB struct {
	mu       sync.Mutex
	stored   json.RawMessage // nil: nothing stored
	saves    int
	loads    int
	failSave bool
}

var errSaveRefused = errors.New("e2e: save hook refused")

func (h *hookDB) save(_ *http.Request, state layoutmgr.LayoutState) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.saves++
	if h.failSave {
		return errSaveRefused
	}
	buf, err := json.Marshal(state)
	if err != nil {
		return err
	}
	h.stored = buf
	return nil
}

func (h *hookDB) load(*http.Request) (layoutmgr.LayoutState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loads++
	var state layoutmgr.LayoutState
	if h.stored == nil {
		return state, nil
	}
	err := json.Unmarshal(h.stored, &state)
	return state, err
}

// --- signed-in users ------------------------------------------------------------

type e2eUser struct{ id uint }

func (u e2eUser) GetID() uint                                { return u.id }
func (u e2eUser) GetIdentifier() string                      { return "e2e" }
func (u e2eUser) GetPasswordHash() string                    { return "" }
func (u e2eUser) GetByID(uint) (user.IUser, error)           { return u, nil }
func (u e2eUser) GetByIdentifier(string) (user.IUser, error) { return u, nil }

const (
	// userCookie names the user a browser is signed in as. Each new browser
	// (Playwright context, Go cookie jar) gets a user of its own, so its
	// layouts start empty; a test can share one between browsers to play
	// one user on two devices.
	userCookie = "e2e-user"
	// anonymousCookie set to 1 leaves a browser signed out.
	anonymousCookie = "e2e-anonymous"
)

var lastUserID atomic.Uint64

// signedIn signs each browser in as its own user, as a production panel's
// session middleware would.
func signedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(anonymousCookie); err == nil && c.Value == "1" {
			next.ServeHTTP(w, r)
			return
		}
		var id uint64
		if c, err := r.Cookie(userCookie); err == nil {
			id, _ = strconv.ParseUint(c.Value, 10, 32)
		}
		if id == 0 {
			id = lastUserID.Add(1)
			http.SetCookie(w, &http.Cookie{Name: userCookie, Value: strconv.FormatUint(id, 10), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			r.AddCookie(&http.Cookie{Name: userCookie, Value: strconv.FormatUint(id, 10)})
		}
		ctx := context.WithValue(r.Context(), ctxkeys.User, user.IUser(e2eUser{id: uint(id)}))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- test-only routes ----------------------------------------------------------

// StateDoc is the body of GET and POST /e2e/state for one page and the
// requesting browser's user. GET answers the draft edit mode shows and the
// committed layout view mode shows. POST sets them: a draft (absent or null:
// no unsaved edits) and a committed layout (absent: unchanged).
type StateDoc struct {
	Draft     json.RawMessage `json:"draft"`
	Committed json.RawMessage `json:"committed"`
}

// HooksDoc is the body of GET and POST /e2e/hooks. On POST, DB replaces the
// stored layout (null clears it) when present, and FailSave when present.
type HooksDoc struct {
	DB       json.RawMessage `json:"db"`
	Saves    int             `json:"saves"`
	Loads    int             `json:"loads"`
	FailSave *bool           `json:"failSave,omitempty"`
}

func (a *App) registerTestRoutes() {
	a.Mux.HandleFunc("GET /e2e/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /e2e/reset restores everything the server holds between tests:
	// render counts, the hooks database and the source's keys. Layout state
	// lives in the browser's cookies, so a fresh browser context starts empty.
	a.Mux.HandleFunc("POST /e2e/reset", func(w http.ResponseWriter, _ *http.Request) {
		a.renders.reset()
		a.hooks.mu.Lock()
		a.hooks.stored, a.hooks.saves, a.hooks.loads, a.hooks.failSave = nil, 0, 0, false
		a.hooks.mu.Unlock()
		a.sourceMu.Lock()
		a.sourceKeys = slices.Clone(defaultSourceKeys)
		a.sourceMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /e2e/renders: how many times each fixture block rendered since the
	// last reset ("table-query" counts the table's row queries).
	a.Mux.HandleFunc("GET /e2e/renders", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, a.renders.snapshot())
	})

	// GET /e2e/state?page=<slug>: the layouts the page keeps for this
	// browser's user.
	a.Mux.Handle("GET /e2e/state", signedIn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := a.layoutPage(w, r)
		if !ok {
			return
		}
		draft, committed, err := page.Layouts(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var doc StateDoc
		doc.Draft, _ = json.Marshal(draft)
		doc.Committed, _ = json.Marshal(committed)
		writeJSON(w, doc)
	})))

	// POST /e2e/state?page=<slug>: plant layouts for this browser's user, as
	// an earlier visit (or an administrator) would have left them.
	a.Mux.Handle("POST /e2e/state", signedIn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := a.layoutPage(w, r)
		if !ok {
			return
		}
		var doc StateDoc
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		draft, err := decodeState(doc.Draft)
		if err == nil {
			var committed *layoutmgr.LayoutState
			if committed, err = decodeState(doc.Committed); err == nil {
				page.SetLayouts(w, r, draft, committed)
			}
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	// GET /e2e/blank: an empty document on the app's origin, signed in, for
	// specs that run fetch with the browser's cookies before opening a page.
	a.Mux.Handle("GET /e2e/blank", signedIn(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>blank</title>")
	})))

	a.Mux.HandleFunc("GET /e2e/hooks", func(w http.ResponseWriter, _ *http.Request) {
		a.hooks.mu.Lock()
		fail := a.hooks.failSave
		doc := HooksDoc{DB: a.hooks.stored, Saves: a.hooks.saves, Loads: a.hooks.loads, FailSave: &fail}
		a.hooks.mu.Unlock()
		if doc.DB == nil {
			doc.DB = json.RawMessage("null")
		}
		writeJSON(w, doc)
	})

	a.Mux.HandleFunc("POST /e2e/hooks", func(w http.ResponseWriter, r *http.Request) {
		var doc HooksDoc
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.hooks.mu.Lock()
		if doc.DB != nil {
			if string(doc.DB) == "null" {
				a.hooks.stored = nil
			} else {
				a.hooks.stored = slices.Clone(doc.DB)
			}
		}
		if doc.FailSave != nil {
			a.hooks.failSave = *doc.FailSave
		}
		a.hooks.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /e2e/source {"keys": [...]}: the keys the source page's BlockSource
	// offers from the next request on.
	a.Mux.HandleFunc("POST /e2e/source", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Keys []string `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.sourceMu.Lock()
		a.sourceKeys = body.Keys
		a.sourceMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
}

func (a *App) layoutPage(w http.ResponseWriter, r *http.Request) (*layoutmgr.LayoutManagerPage, bool) {
	page, ok := a.pages[r.URL.Query().Get("page")]
	if !ok {
		http.Error(w, "unknown page", http.StatusBadRequest)
	}
	return page, ok
}

// decodeState reads a planted layout; absent or null is nil. The legacy
// single-layout shape ({"blocks": [...]}) is accepted, as on load.
func decodeState(raw json.RawMessage) (*layoutmgr.LayoutState, error) {
	if raw == nil || string(raw) == "null" {
		return nil, nil
	}
	var state layoutmgr.LayoutState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
