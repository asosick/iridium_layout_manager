package layoutmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/goiridium/iridium/core/auth/user"
	"github.com/goiridium/iridium/core/context/ctxkeys"
)

type testUser struct{ id uint }

func (u testUser) GetID() uint                                { return u.id }
func (u testUser) GetIdentifier() string                      { return "test" }
func (u testUser) GetPasswordHash() string                    { return "" }
func (u testUser) GetByID(uint) (user.IUser, error)           { return u, nil }
func (u testUser) GetByIdentifier(string) (user.IUser, error) { return u, nil }

func signedIn(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxkeys.User, user.IUser(testUser{id: 7})))
}

// Handler behaviour is tested through the real panel in integration_test.go
// and in the browser (test/e2e). These cover what no route test reaches: the
// page's own URL state handed to widgets on a page switch, and hooks that
// hand out the application's own slices (the test app's hooks keep JSON, so
// every load there is a fresh copy).

func TestHandleSelectRendersWidgetsWithCurrentPageQuery(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	state := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "table-id", Type: "table"}}}}}
	p.SetLayouts(httptest.NewRecorder(), signedIn(httptest.NewRequest(http.MethodGet, "/", nil)), nil, &state)

	var renderedQuery string
	p.blocks = []BlockSpec{DynamicBlock("table", "Table", func(_ http.ResponseWriter, r *http.Request) templ.Component {
		renderedQuery = r.URL.RawQuery
		return templ.NopComponent
	})}

	r := signedIn(httptest.NewRequest(http.MethodGet, "/lm/select?layout=0", nil))
	r.Header.Set("HX-Current-URL", "https://example.test/dashboard?table-id_search=worker&layout=1")
	w := httptest.NewRecorder()
	p.handleSelect(w, r)

	query, err := url.ParseQuery(renderedQuery)
	if err != nil {
		t.Fatalf("parse rendered query: %v", err)
	}
	if got := query.Get("table-id_search"); got != "worker" {
		t.Fatalf("expected current table state, got %q", got)
	}
	if got := query.Get("layout"); got != "0" {
		t.Fatalf("expected selector layout to win, got %q", got)
	}
}

func TestWithCurrentPageQueryFallsBackForMalformedURL(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/lm/select?layout=1", nil)
	r.Header.Set("HX-Current-URL", ":// malformed")

	if got := withCurrentPageQuery(r); got != r {
		t.Fatal("expected malformed current URL to leave the request unchanged")
	}
}

// storedLayoutPage is a page whose SaveHook keeps the LayoutState it is given
// and whose LoadHook hands that value back as it is, as an application that
// keeps layouts in a settings struct or a cache does: every load shares the
// stored layout's slices.
func storedLayoutPage(stored *LayoutState) *LayoutManagerPage {
	return NewLayoutManagerPage("Dashboard", "dashboard").
		Blocks(StaticBlock("note", "Note", templ.NopComponent)).
		GridColumnsFixed(3).
		LoadHook(func(*http.Request) (LayoutState, error) { return *stored, nil }).
		SaveHook(func(_ *http.Request, state LayoutState) error {
			*stored = state
			return nil
		})
}

// serve sends one request of the signed-in user to a handler, as the page
// does, and fails the test unless it succeeds.
func serve(t *testing.T, handler http.HandlerFunc, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler(w, signedIn(r))
	if w.Code >= http.StatusBadRequest {
		t.Fatalf("%s %s: %d %s", method, target, w.Code, strings.TrimSpace(w.Body.String()))
	}
	return w
}

var renderedBlock = regexp.MustCompile(`data-lm-block data-id="([^"]+)" data-cols="(\d+)"`)

// blocksShown lists the blocks a rendered grid or block shows, as id@cols.
func blocksShown(html string) []string {
	var out []string
	for _, m := range renderedBlock.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1]+"@"+m[2])
	}
	return out
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

// S21: hooks may hand out the layout they store as it is. The first edit
// after Done changes the draft, not the stored layout, so Cancel, which reads
// the LoadHook again, shows what was saved.
func TestS21_EditsAfterDoneLeaveTheStoredLayoutAlone(t *testing.T) {
	add := func(t *testing.T, p *LayoutManagerPage) string {
		t.Helper()
		body := serve(t, p.handleAdd, http.MethodPost, "/lm/add?type=note&layout=0", nil).Body.String()
		m := renderedBlock.FindAllStringSubmatch(body, -1)
		if len(m) != 1 {
			t.Fatalf("add answered %d blocks, want the new one", len(m))
		}
		return m[0][1]
	}
	resize := func(t *testing.T, p *LayoutManagerPage, id string) {
		t.Helper()
		serve(t, p.handleResize, http.MethodPost, "/lm/resize?id="+id+"&cols=3&layout=0", nil)
	}

	// Each edit is made to a stored layout [a, b] of one-column blocks.
	edits := []struct {
		name string
		edit func(t *testing.T, p *LayoutManagerPage, a, b string)
	}{
		{"add, then resize", func(t *testing.T, p *LayoutManagerPage, a, _ string) { add(t, p); resize(t, p, a) }},
		{"resize, then add", func(t *testing.T, p *LayoutManagerPage, a, _ string) { resize(t, p, a); add(t, p) }},
		{"remove", func(t *testing.T, p *LayoutManagerPage, a, _ string) {
			serve(t, p.handleRemove, http.MethodPost, "/lm/remove?id="+a+"&layout=0", nil)
		}},
		{"move", func(t *testing.T, p *LayoutManagerPage, a, _ string) {
			serve(t, p.handleMove, http.MethodPost, "/lm/move?id="+a+"&delta=1&layout=0", nil)
		}},
		{"reorder", func(t *testing.T, p *LayoutManagerPage, a, b string) {
			serve(t, p.handleReorder, http.MethodPost, "/lm/reorder?layout=0", url.Values{"order": {b, a}})
		}},
	}
	for _, tc := range edits {
		t.Run(tc.name, func(t *testing.T) {
			var stored LayoutState
			p := storedLayoutPage(&stored)
			a, b := add(t, p), add(t, p)
			serve(t, p.handleSave, http.MethodPost, "/lm/save", url.Values{"layout": {"0"}}) // Done
			saved := jsonOf(t, stored)

			tc.edit(t, p, a, b)
			draft, _, err := p.Layouts(httptest.NewRecorder(), signedIn(httptest.NewRequest(http.MethodGet, "/", nil)))
			if err != nil || jsonOf(t, draft) == saved {
				t.Fatalf("the edit did not reach the draft (%v)", err)
			}
			if got := jsonOf(t, stored); got != saved {
				t.Errorf("an unsaved edit changed the stored layout:\nsaved %s\nnow   %s", saved, got)
			}

			grid := serve(t, p.handleReset, http.MethodPost, "/lm/reset?layout=0", nil).Body.String() // Cancel
			if got, want := blocksShown(grid), []string{a + "@1", b + "@1"}; !slices.Equal(got, want) {
				t.Errorf("Cancel shows %v, want the stored %v", got, want)
			}
		})
	}
}

// S21: showing a layout saved when the grid had more columns clamps the spans
// it shows, not the ones the application stored.
func TestS21_ShowingALayoutLeavesTheStoredSpansAlone(t *testing.T) {
	stored := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "six", Type: "note", Cols: 6}}}}}
	p := storedLayoutPage(&stored)
	grid := serve(t, p.handleSelect, http.MethodGet, "/lm/select?layout=0&mode=view", nil).Body.String()
	if got := blocksShown(grid); !slices.Equal(got, []string{"six@3"}) {
		t.Fatalf("view mode shows %v, want six at the grid's 3 columns", got)
	}
	if cols := stored.Layouts[0].Blocks[0].Cols; cols != 6 {
		t.Fatalf("showing the layout changed the stored span to %d", cols)
	}
}

func TestPageNamesAreTidied(t *testing.T) {
	for typed, want := range map[string]string{
		"  Cameras\t":                   "Cameras",
		"North\nWing  2":                "North Wing 2",
		"bell\x07":                      "bell",
		"   ":                           "",
		strings.Repeat("x", 45):         strings.Repeat("x", 40),
		strings.Repeat("é", 39) + " ab": strings.Repeat("é", 39),
	} {
		if got := pageName(typed); got != want {
			t.Errorf("pageName(%q) = %q, want %q", typed, got, want)
		}
	}
}

func TestPagesThatCannotBeRenamedRefuseANewName(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard").RenamablePages(false)
	r := httptest.NewRequest(http.MethodPost, "/lm/pages/rename?layout=0", strings.NewReader("name=Overview"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	p.handleRenamePage(w, signedIn(r))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rename answered %d, want 400", w.Code)
	}
	draft, _, _ := p.Layouts(httptest.NewRecorder(), signedIn(httptest.NewRequest(http.MethodGet, "/", nil)))
	if draft.Layouts[0].Name != "" {
		t.Fatalf("the page is named %q", draft.Layouts[0].Name)
	}
}
