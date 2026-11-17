package layoutmgr_test

// Integration tests: every request goes through the real Iridium panel
// (CSRF, Spa, error pages, security headers) to the routes the plugin
// registers. See e2e_client_test.go for the client; each client is a user of
// its own.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	layoutmgr "github.com/asosick/iridium_layout_manager"
	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

const page = e2eapp.PageDefault

// threeLayouts is a layout with three blocks on each of the page's three
// pages: L0-a, L0-b, L0-c, L1-a, ...
func threeLayouts() layoutmgr.LayoutState {
	var s layoutmgr.LayoutState
	for l := 0; l < 3; l++ {
		var blocks []layoutmgr.Block
		for _, name := range []string{"a", "b", "c"} {
			blocks = append(blocks, layoutmgr.Block{ID: fmt.Sprintf("L%d-%s", l, name), Type: "short", Cols: 1})
		}
		s.Layouts = append(s.Layouts, layoutmgr.Layout{Blocks: blocks})
	}
	return s
}

func layoutJSON(t *testing.T, s stored, i int) string {
	t.Helper()
	buf, err := json.Marshal(s.layout(i))
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

// Requests the plugin must refuse are refused, and leave the layout alone.
func TestHandlersRefuseBadRequestsWithoutTouchingTheLayout(t *testing.T) {
	reset(t)
	c := newClient(t)
	a := c.addBlock(page, 0, "short")
	c.addBlock(page, 0, "tall")
	before := c.draft(page).raw

	cases := []struct {
		name, method, path, form string
		want                     int
	}{
		{"add with GET", http.MethodGet, lm(page, "add"), "", http.StatusMethodNotAllowed},
		{"add an unknown type", http.MethodPost, lm(page, "add"), "type=nope&layout=0", http.StatusBadRequest},
		{"add without a type", http.MethodPost, lm(page, "add"), "layout=0", http.StatusBadRequest},
		{"remove with GET", http.MethodGet, lm(page, "remove", "id="+a), "", http.StatusMethodNotAllowed},
		{"remove without an id", http.MethodPost, lm(page, "remove"), "", http.StatusBadRequest},
		{"resize with GET", http.MethodGet, lm(page, "resize", "id="+a, "cols=2"), "", http.StatusMethodNotAllowed},
		{"resize without an id", http.MethodPost, lm(page, "resize", "cols=2"), "", http.StatusBadRequest},
		{"resize an unknown block", http.MethodPost, lm(page, "resize", "id=nope", "cols=2"), "", http.StatusBadRequest},
		{"resize without cols or delta", http.MethodPost, lm(page, "resize", "id="+a), "", http.StatusBadRequest},
		{"resize to non-numeric cols", http.MethodPost, lm(page, "resize", "id="+a, "cols=wide"), "", http.StatusBadRequest},
		{"resize by a non-numeric delta", http.MethodPost, lm(page, "resize", "id="+a, "delta=up"), "", http.StatusBadRequest},
		{"move with GET", http.MethodGet, lm(page, "move", "id="+a, "delta=1"), "", http.StatusMethodNotAllowed},
		{"move without an id", http.MethodPost, lm(page, "move", "delta=1"), "", http.StatusBadRequest},
		{"move by two", http.MethodPost, lm(page, "move", "id="+a, "delta=2"), "", http.StatusBadRequest},
		{"move by zero", http.MethodPost, lm(page, "move", "id="+a, "delta=0"), "", http.StatusBadRequest},
		{"move by a non-numeric delta", http.MethodPost, lm(page, "move", "id="+a, "delta=left"), "", http.StatusBadRequest},
		{"reorder with GET", http.MethodGet, lm(page, "reorder", "layout=0"), "", http.StatusMethodNotAllowed},
		{"save with GET", http.MethodGet, lm(page, "save"), "", http.StatusMethodNotAllowed},
		{"reset with GET", http.MethodGet, lm(page, "reset"), "", http.StatusMethodNotAllowed},
		// Idempotent no-ops: a double-clicked remove, a move past the edge.
		{"remove a block that is already gone", http.MethodPost, lm(page, "remove", "id=nope", "layout=0"), "", http.StatusNoContent},
		{"move the first block left", http.MethodPost, lm(page, "move", "id="+a, "delta=-1", "layout=0"), "", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r response
			if tc.method == http.MethodGet {
				r = c.get(tc.path)
			} else {
				form, _ := url.ParseQuery(tc.form)
				r = c.postForm(tc.path, form)
			}
			if r.status != tc.want {
				t.Fatalf("%s %s: %d, want %d (%s)", tc.method, tc.path, r.status, tc.want, strings.TrimSpace(r.body))
			}
			if after := c.draft(page).raw; after != before {
				t.Fatalf("the layout changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

var blockMarkup = regexp.MustCompile(`data-lm-block data-id="([^"]+)"`)

// Each edit changes the layout it names and no other, and answers only what
// the page needs to update itself: nothing, or the one block added.
func TestEditsChangeOnlyTheLayoutTheyNameAndAnswerNoGrid(t *testing.T) {
	reset(t)
	for _, target := range []int{0, 2} {
		l := fmt.Sprint(target)
		id := func(name string) string { return fmt.Sprintf("L%d-%s", target, name) }
		ops := []struct {
			name string
			send func(c *client) response
			want []string // expected IDs on the target layout afterwards
		}{
			{"add", func(c *client) response { return c.add(page, target, "tall") }, nil},
			{"remove", func(c *client) response {
				return c.postForm(lm(page, "remove", "id="+id("b"), "layout="+l), nil)
			}, []string{id("a"), id("c")}},
			{"resize", func(c *client) response {
				return c.postForm(lm(page, "resize", "id="+id("a"), "cols=2", "layout="+l), nil)
			}, []string{id("a"), id("b"), id("c")}},
			{"move", func(c *client) response {
				return c.postForm(lm(page, "move", "id="+id("c"), "delta=-1", "layout="+l), nil)
			}, []string{id("a"), id("c"), id("b")}},
			{"reorder", func(c *client) response {
				return c.postForm(lm(page, "reorder", "layout="+l), url.Values{"order": {id("c"), id("b"), id("a")}})
			}, []string{id("c"), id("b"), id("a")}},
		}
		for _, op := range ops {
			t.Run(fmt.Sprintf("%s on layout %d", op.name, target), func(t *testing.T) {
				c := newClient(t)
				c.plant(page, threeLayouts())
				before := c.draft(page)

				r := op.send(c)
				after := c.draft(page)
				for other := 0; other < 3; other++ {
					if other == target {
						continue
					}
					if got, want := layoutJSON(t, after, other), layoutJSON(t, before, other); got != want {
						t.Fatalf("layout %d changed:\nbefore %s\nafter  %s", other, want, got)
					}
				}
				got := after.ids(target)
				if op.want == nil { // add: the old blocks plus one new one, answered alone
					if r.status != http.StatusOK || len(got) != 4 || !slices.Equal(got[:3], before.ids(target)) {
						t.Fatalf("add: %d, layout %d is %v", r.status, target, got)
					}
					answered := blockMarkup.FindAllStringSubmatch(r.body, -1)
					if len(answered) != 1 || answered[0][1] != got[3] {
						t.Fatalf("add answered %d blocks, want the new one only", len(answered))
					}
					if !strings.Contains(r.body, "layout="+l) {
						t.Fatalf("the new block's controls do not target layout %d", target)
					}
					return
				}
				if r.status != http.StatusNoContent || r.body != "" {
					t.Fatalf("%s answered %d with %d bytes, want 204 and no body", op.name, r.status, len(r.body))
				}
				if !slices.Equal(got, op.want) {
					t.Fatalf("layout %d: %v, want %v", target, got, op.want)
				}
				if op.name == "resize" {
					if after.layout(target)[0].Cols != 2 || r.header.Get("X-Layout-Manager-Cols") != "2" {
						t.Fatalf("resize: stored %d cols, answered %q", after.layout(target)[0].Cols, r.header.Get("X-Layout-Manager-Cols"))
					}
				}
			})
		}
	}
}

// What a reorder does with an order the client should never send.
func TestReorderWithPartialDuplicateForeignOrEmptyOrders(t *testing.T) {
	reset(t)
	cases := []struct {
		name  string
		order []string
		want  []string
	}{
		{"partial: listed blocks first, the rest keep their order", []string{"L0-c"}, []string{"L0-c", "L0-a", "L0-b"}},
		{"duplicates count once", []string{"L0-b", "L0-b", "L0-a", "L0-b"}, []string{"L0-b", "L0-a", "L0-c"}},
		{"another layout's and unknown IDs are ignored", []string{"L1-a", "zzz", "L0-c", "L0-a"}, []string{"L0-c", "L0-a", "L0-b"}},
		{"no order changes nothing", nil, []string{"L0-a", "L0-b", "L0-c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t)
			c.plant(page, threeLayouts())
			if r := c.postForm(lm(page, "reorder", "layout=0"), url.Values{"order": tc.order}); r.status != http.StatusNoContent {
				t.Fatalf("%d %s", r.status, r.body)
			}
			d := c.draft(page)
			if got := d.ids(0); !slices.Equal(got, tc.want) {
				t.Fatalf("layout 0: %v, want %v", got, tc.want)
			}
			if got := d.ids(1); !slices.Equal(got, []string{"L1-a", "L1-b", "L1-c"}) {
				t.Fatalf("layout 1 changed: %v", got)
			}
		})
	}
}

// Edits are a draft, shown in edit mode; view mode shows what Done
// committed; Reset returns the draft to it.
func TestDraftDoneAndResetSequences(t *testing.T) {
	reset(t)
	c := newClient(t)
	reset0 := func() response {
		t.Helper()
		r := c.postForm(lm(page, "reset"), url.Values{"layout": {"0"}})
		if r.status != http.StatusOK {
			t.Fatalf("reset: %d %s", r.status, r.body)
		}
		return r
	}
	done0 := func() {
		t.Helper()
		if r := c.postForm(lm(page, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusNoContent {
			t.Fatalf("done: %d %s", r.status, r.body)
		}
	}
	viewIDs := func() []string { return gridIDs(c.page(page).body) }

	// Reset before anything happened: nothing to restore.
	if r := reset0(); len(gridIDs(r.body)) != 0 {
		t.Fatalf("reset of an empty page rendered %v", gridIDs(r.body))
	}

	// An edit is a draft: view mode still shows the (empty) committed layout.
	c.addBlock(page, 0, "short")
	if ids := viewIDs(); len(ids) != 0 {
		t.Fatalf("view mode shows the unsaved draft %v", ids)
	}
	if r := reset0(); len(gridIDs(r.body)) != 0 {
		t.Fatalf("reset rendered %v, want the empty committed layout", gridIDs(r.body))
	}
	if ids := c.draft(page).ids(0); len(ids) != 0 {
		t.Fatalf("draft after reset: %v", ids)
	}

	// Done commits; later edits stay a draft that view mode does not show.
	a := c.addBlock(page, 0, "short")
	done0()
	if ids := c.committed(page).ids(0); !slices.Equal(ids, []string{a}) {
		t.Fatalf("committed after Done: %v", ids)
	}
	b := c.addBlock(page, 0, "tall")
	if ids := c.committed(page).ids(0); !slices.Equal(ids, []string{a}) {
		t.Fatalf("an edit after Done changed the commit: %v", ids)
	}
	if ids := viewIDs(); !slices.Equal(ids, []string{a}) {
		t.Fatalf("view mode shows %v, want the committed [%s]", ids, a)
	}
	if ids := gridIDs(c.editGrid(page, 0).body); !slices.Equal(ids, []string{a, b}) {
		t.Fatalf("edit mode shows %v, want the draft", ids)
	}
	if r := reset0(); !slices.Equal(gridIDs(r.body), []string{a}) {
		t.Fatalf("reset rendered %v, want [%s] (dropping %s)", gridIDs(r.body), a, b)
	}
	if ids := gridIDs(c.editGrid(page, 0).body); !slices.Equal(ids, []string{a}) {
		t.Fatalf("edit mode after reset shows %v", ids)
	}
}

func hooksDoc(t *testing.T) e2eapp.HooksDoc {
	t.Helper()
	srv, _ := servers(t)
	resp, err := srv.Client().Get(srv.URL + "/e2e/hooks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc e2eapp.HooksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func setHooks(t *testing.T, body map[string]any) {
	t.Helper()
	srv, _ := servers(t)
	buf, _ := json.Marshal(body)
	resp, err := srv.Client().Post(srv.URL+"/e2e/hooks", "application/json", strings.NewReader(string(buf)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /e2e/hooks: %d", resp.StatusCode)
	}
}

// With both hooks the application's storage holds the committed layout:
// view mode and new drafts read it, Done writes it (and commits nothing when
// it fails), and an emptied draft stays empty.
func TestHooksHoldTheCommittedLayout(t *testing.T) {
	reset(t)
	const hooks = e2eapp.PageHooks
	setHooks(t, map[string]any{"db": map[string]any{"layouts": []any{
		map[string]any{"blocks": []any{map[string]any{"id": "db-1", "type": "tall", "cols": 2}}},
	}}})

	c := newClient(t)
	if got := gridBlocks(c.page(hooks).body); len(got) != 1 || got[0] != (renderedBlock{id: "db-1", cols: 2}) {
		t.Fatalf("a new user sees %+v, want the stored db-1 at 2 cols", got)
	}

	// The first edit starts from the stored layout; view mode keeps showing it.
	x := c.addBlock(hooks, 0, "short")
	if ids := c.draft(hooks).ids(0); !slices.Equal(ids, []string{"db-1", x}) {
		t.Fatalf("draft after the first edit: %v", ids)
	}
	if ids := gridIDs(c.page(hooks).body); !slices.Equal(ids, []string{"db-1"}) {
		t.Fatalf("view mode shows %v, want the stored layout", ids)
	}

	setHooks(t, map[string]any{"failSave": true})
	if r := c.postForm(lm(hooks, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusInternalServerError {
		t.Fatalf("Done with a failing SaveHook: %d, want 500", r.status)
	}
	doc := hooksDoc(t)
	if doc.Saves != 1 || !strings.Contains(string(doc.DB), `"db-1"`) || strings.Contains(string(doc.DB), x) {
		t.Fatalf("after the failed save: %d save calls, database %s", doc.Saves, doc.DB)
	}
	if ids := c.draft(hooks).ids(0); !slices.Equal(ids, []string{"db-1", x}) {
		t.Fatalf("the failed save lost the draft: %v", ids)
	}
	if ids := c.committed(hooks).ids(0); !slices.Equal(ids, []string{"db-1"}) {
		t.Fatalf("the failed save committed: %v", ids)
	}

	setHooks(t, map[string]any{"failSave": false})
	if r := c.postForm(lm(hooks, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusNoContent {
		t.Fatalf("Done: %d %s", r.status, r.body)
	}
	doc = hooksDoc(t)
	if doc.Saves != 2 || !strings.Contains(string(doc.DB), x) {
		t.Fatalf("after Done: %d save calls, database %s", doc.Saves, doc.DB)
	}
	if ids := gridIDs(c.page(hooks).body); !slices.Equal(ids, []string{"db-1", x}) {
		t.Fatalf("view mode after Done shows %v", ids)
	}

	// A draft emptied on purpose stays empty: it does not fall back to the
	// stored layout.
	for _, id := range []string{"db-1", x} {
		if r := c.postForm(lm(hooks, "remove", "id="+id, "layout=0"), nil); r.status != http.StatusNoContent {
			t.Fatalf("remove %s: %d", id, r.status)
		}
	}
	if ids := gridIDs(c.editGrid(hooks, 0).body); len(ids) != 0 {
		t.Fatalf("an emptied draft shows %v", ids)
	}
}

func setSourceKeys(t *testing.T, keys ...string) {
	t.Helper()
	srv, _ := servers(t)
	buf, _ := json.Marshal(map[string]any{"keys": keys})
	resp, err := srv.Client().Post(srv.URL+"/e2e/source", "application/json", strings.NewReader(string(buf)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

// optionValues lists the block types the Add dropdown offers.
func optionValues(html string) []string {
	start := strings.Index(html, "data-lm-type-select")
	if start < 0 {
		return nil
	}
	end := strings.Index(html[start:], "</select>")
	var out []string
	for _, m := range optionPattern.FindAllStringSubmatch(html[start:start+end], -1) {
		out = append(out, m[1])
	}
	return out
}

var optionPattern = regexp.MustCompile(`<option value="([^"]*)"`)

// A placed block whose key the BlockSource stops offering renders as missing
// and can still be removed.
func TestABlockSourceKeyThatDisappearsRendersMissingAndCanBeRemoved(t *testing.T) {
	reset(t)
	const source = e2eapp.PageSource
	c := newClient(t)
	// Blocks first, then the source's, in the source's order.
	if got := optionValues(c.page(source).body); !slices.Equal(got, []string{"note", "cam-1", "cam-2"}) {
		t.Fatalf("offered %v", got)
	}
	cam := c.addBlock(source, 0, "cam-2")
	if body := c.editGrid(source, 0).body; !strings.Contains(body, `data-e2e-kind="cam-2"`) {
		t.Fatal("the source block did not render")
	}

	setSourceKeys(t, "cam-1")
	if strings.Contains(c.page(source).body, `<option value="cam-2"`) {
		t.Fatal("the vanished block is still offered")
	}
	body := c.editGrid(source, 0).body
	if !strings.Contains(body, "Block type <code>cam-2</code> is no longer available") {
		t.Fatal("the vanished block does not render as missing")
	}
	if !slices.Equal(gridIDs(body), []string{cam}) {
		t.Fatalf("grid shows %v", gridIDs(body))
	}

	if r := c.postForm(lm(source, "remove", "id="+cam, "layout=0"), nil); r.status != http.StatusNoContent {
		t.Fatalf("remove: %d", r.status)
	}
	if ids := c.draft(source).ids(0); len(ids) != 0 {
		t.Fatalf("draft after remove: %v", ids)
	}
}

// Spans saved when the grid had more columns are clamped when the page is
// shown, and the clamped value is what later edits start from.
func TestSpansClampToAGridThatLostColumns(t *testing.T) {
	reset(t)
	c := newClient(t)
	c.plant(page, map[string]any{"layouts": []any{map[string]any{"blocks": []any{
		map[string]any{"id": "six", "type": "short", "cols": 6},
		map[string]any{"id": "two", "type": "short", "cols": 2},
	}}}})

	got := gridBlocks(c.page(page).body)
	want := []renderedBlock{{"six", 3}, {"two", 2}}
	if !slices.Equal(got, want) {
		t.Fatalf("rendered %+v, want %+v", got, want)
	}
	r := c.postForm(lm(page, "resize", "id=six", "delta=-1", "layout=0"), nil)
	if r.status != http.StatusNoContent || r.header.Get("X-Layout-Manager-Cols") != "2" {
		t.Fatalf("resize: %d, cols %q", r.status, r.header.Get("X-Layout-Manager-Cols"))
	}
	if b := c.draft(page).layout(0); b[0].Cols != 2 || b[1].Cols != 2 {
		t.Fatalf("after shrinking the clamped block: %+v", b)
	}
}
