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
	"strings"
	"sync"
	"sync/atomic"
	"testing/fstest"

	"github.com/a-h/templ"
	"github.com/iridiumgo/iridium/bootstrap/auth"
	iridiumconfig "github.com/iridiumgo/iridium/config"
	"github.com/iridiumgo/iridium/core/asset"
	"github.com/iridiumgo/iridium/core/auth/user"
	"github.com/iridiumgo/iridium/core/context/ctxkeys"
	"github.com/iridiumgo/iridium/core/iridium"
	pagepanel "github.com/iridiumgo/iridium/core/page/panel"
	"github.com/iridiumgo/iridium/core/page/panel/IPanel"
	"github.com/iridiumgo/iridium/core/panel"
	"github.com/iridiumgo/iridium/network/middleware"

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
	PageElsewhere = "elsewhere" // a plain page to navigate away to
)

// LayoutPages are the slugs of every LayoutManagerPage.
var LayoutPages = []string{
	PageDefault, PageSingle, PageNarrow, PageWide, PageNoLock,
	PageNoReorder, PageNoResize, PageZen, PageHooks, PageSource,
}

// The plugin's default persistence: the working draft and the snapshot Done
// commits, each in its own gorilla session, keyed by the page's slug (which
// Iridium stores with a leading slash).
const (
	draftSession     = "layout-manager"
	committedSession = "layout-manager-committed"
)

func draftKey(slug string) string     { return "layout:/" + slug }
func committedKey(slug string) string { return "layout:/" + slug + ":committed" }

//go:embed static
var staticFS embed.FS

var created atomic.Bool

// App is the test application.
type App struct {
	Mux *http.ServeMux

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
		renders:    newCounters(),
		hooks:      &hookDB{},
		sourceKeys: slices.Clone(defaultSourceKeys),
	}

	cfg := iridiumconfig.NewIridiumConfig()
	cfg.AppName = "Layout Manager E2E"
	// Test-only keys - never use in production.
	cfg.AppKey = "dGVzdC1hcHAta2V5LWZvci11bml0LXRlc3Rpbmctb25seS10ZXN0aW5n"
	cfg.AppEncryptionKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	cfg.AppEnv = "test"
	cfg.CSPMode = true
	// Errors only: the plugin logs a failed save at error level, which is
	// worth seeing next to a failing test; everything else is noise.
	cfg.InternalLogLevel = "error"

	// Without a favicon the browser's /favicon.ico request logs a 404.
	asset.Favicon(fstest.MapFS{"favicon.svg": &fstest.MapFile{Data: []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7" fill="#64748b"/></svg>`,
	)}}, "favicon.svg")
	asset.Register(asset.CSS("e2e-fixtures", staticFS, "static/fixtures.css").Package("e2e"))

	iridium.NewIridiumApp(cfg).
		RegisterPanels(a.panel()).
		Initialize(a.Mux)

	a.registerTestRoutes()
	return a
}

func (a *App) panel() *panel.Panel {
	both := func() []layoutmgr.BlockSpec { return append(a.fixtureBlocks(), a.widgetBlocks()...) }
	simple := func() []layoutmgr.BlockSpec { return a.fixtureBlocks() }

	pages := []*layoutmgr.LayoutManagerPage{
		layoutmgr.NewLayoutManagerPage("Default", PageDefault).Blocks(both()...).GridColumns(3),
		layoutmgr.NewLayoutManagerPage("Single", PageSingle).Blocks(simple()...).GridColumns(3).LayoutCount(1),
		layoutmgr.NewLayoutManagerPage("Narrow", PageNarrow).Blocks(simple()...).GridColumns(1),
		layoutmgr.NewLayoutManagerPage("Wide", PageWide).Blocks(simple()...).GridColumns(6),
		layoutmgr.NewLayoutManagerPage("No lock", PageNoLock).Blocks(simple()...).GridColumns(3).ShowLockButton(false),
		layoutmgr.NewLayoutManagerPage("No reorder", PageNoReorder).Blocks(simple()...).GridColumns(3).Reorderable(false),
		layoutmgr.NewLayoutManagerPage("No resize", PageNoResize).Blocks(simple()...).GridColumns(3).Resizable(false),
		layoutmgr.NewLayoutManagerPage("Zen", PageZen).Blocks(simple()...).GridColumns(3).Zen(),
		layoutmgr.NewLayoutManagerPage("Hooks", PageHooks).Blocks(simple()...).GridColumns(3).
			SaveHook(a.hooks.save).LoadHook(a.hooks.load),
		layoutmgr.NewLayoutManagerPage("Source", PageSource).
			Blocks(a.fixtureBlock("note", "Note", 0, "")).
			GridColumns(3).
			BlockSource(a.sourceBlocks),
	}
	items := make([]IPanel.IPanelItem, 0, len(pages)+1)
	for _, p := range pages {
		items = append(items, p)
	}
	items = append(items, pagepanel.NewCustomPanelPage("Elsewhere", PageElsewhere).
		Content(templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, `<p data-e2e-elsewhere>Nothing to arrange here.</p>`)
			return err
		})))

	return panel.NewPanel(a.Mux).
		ID("e2e").
		Path(strings.TrimPrefix(PanelPath, "/")).
		NavStyle(panel.NavLeft).
		PanelItems(items...).
		Middleware(
			middleware.SecurityHeadersMiddleware,
			middleware.CSRFMiddleware,
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

// --- signed-in user ------------------------------------------------------------

type e2eUser struct{ id uint }

func (u e2eUser) GetID() uint                                { return u.id }
func (u e2eUser) GetIdentifier() string                      { return "e2e" }
func (u e2eUser) GetPasswordHash() string                    { return "" }
func (u e2eUser) GetByID(uint) (user.IUser, error)           { return u, nil }
func (u e2eUser) GetByIdentifier(string) (user.IUser, error) { return u, nil }

// signedIn signs every panel request in as user 1, as a production panel's
// session middleware would.
func signedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxkeys.User, user.IUser(e2eUser{id: 1}))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- test-only routes ----------------------------------------------------------

// StateDoc is the body of GET and POST /e2e/state: the raw JSON stored in the
// draft and committed sessions for one page. On POST an absent field is left
// alone and null deletes the stored value.
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
	// GET /e2e/blank: an empty document on the app's origin, for specs that
	// need to run fetch with the browser's cookies before opening a page.
	a.Mux.HandleFunc("GET /e2e/blank", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>blank</title>")
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

	// GET /e2e/state?page=<slug>: the layout state the server will read for
	// the cookies this request carries.
	a.Mux.HandleFunc("GET /e2e/state", func(w http.ResponseWriter, r *http.Request) {
		slug, ok := layoutSlug(w, r)
		if !ok {
			return
		}
		var doc StateDoc
		var err error
		if doc.Draft, err = readSession(r, draftSession, draftKey(slug)); err == nil {
			doc.Committed, err = readSession(r, committedSession, committedKey(slug))
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, doc)
	})

	// POST /e2e/state?page=<slug>: plant raw state in the sessions, as if an
	// earlier version of the app (or another tab) had written it.
	a.Mux.HandleFunc("POST /e2e/state", func(w http.ResponseWriter, r *http.Request) {
		slug, ok := layoutSlug(w, r)
		if !ok {
			return
		}
		var doc StateDoc
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := writeSession(w, r, draftSession, draftKey(slug), doc.Draft); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := writeSession(w, r, committedSession, committedKey(slug), doc.Committed); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

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

func layoutSlug(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := r.URL.Query().Get("page")
	if !slices.Contains(LayoutPages, slug) {
		http.Error(w, "unknown page", http.StatusBadRequest)
		return "", false
	}
	return slug, true
}

func readSession(r *http.Request, name, key string) (json.RawMessage, error) {
	sess, err := auth.Store.Get(r, name)
	if err != nil {
		return nil, err
	}
	raw, ok := sess.Values[key].(string)
	if !ok {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(raw), nil
}

func writeSession(w http.ResponseWriter, r *http.Request, name, key string, raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	// An undecodable cookie still yields a fresh session, which replaces it.
	sess, err := auth.Store.Get(r, name)
	if sess == nil {
		return err
	}
	if string(raw) == "null" {
		delete(sess.Values, key)
	} else {
		sess.Values[key] = string(raw)
	}
	return sess.Save(r, w)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
