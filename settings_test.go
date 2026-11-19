package layoutmgr_test

// The settings dialog (Settings): each page's columns and the space between
// its blocks, through the real panel.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	layoutmgr "github.com/asosick/iridium_layout_manager"
	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

var (
	columnsAttr = regexp.MustCompile(`data-lm-columns="(\d+)"`)
	spacingAttr = regexp.MustCompile(`data-lm-spacing="([a-z]*)"`)
)

// gridSettings reads a rendered grid's columns and chosen spacing ("" for
// none chosen).
func gridSettings(t *testing.T, body string) (columns, spacing string) {
	t.Helper()
	m := columnsAttr.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no columns in %.300q", body)
	}
	if s := spacingAttr.FindStringSubmatch(body); s != nil {
		spacing = s[1]
	}
	return m[1], spacing
}

// spans lists a rendered grid's block spans by block ID.
func spans(body string) map[string]int {
	out := map[string]int{}
	for _, b := range gridBlocks(body) {
		out[b.id] = b.cols
	}
	return out
}

func settings(layout string, form url.Values) string {
	return lm(e2eapp.PageSettings, "settings", "layout="+layout) + "&" + form.Encode()
}

// A page's columns and spacing are edits like any other: the draft changes,
// Done commits it, and the other pages keep theirs. A block as wide as the
// page stays as wide; the others keep their span, up to the new number of
// columns.
func TestSettingsChooseAPagesColumnsAndSpacing(t *testing.T) {
	reset(t)
	const slug = e2eapp.PageSettings
	c := newClient(t)
	c.plant(slug, layoutmgr.LayoutState{Layouts: []layoutmgr.Layout{
		{Blocks: []layoutmgr.Block{
			{ID: "one", Type: "short", Cols: 1},
			{ID: "full", Type: "short", Cols: 3},
			{ID: "two", Type: "short", Cols: 2},
		}},
		{Blocks: []layoutmgr.Block{{ID: "other", Type: "short", Cols: 3}}},
	}})
	if columns, spacing := gridSettings(t, c.editGrid(slug, 0).body); columns != "3" || spacing != "" {
		t.Fatalf("a new page has columns %s, spacing %q: want GridColumns and none chosen", columns, spacing)
	}

	r := c.postForm(settings("0", url.Values{"columns": {"5"}, "spacing": {"compact"}}), nil)
	if r.status != http.StatusOK {
		t.Fatalf("settings: %d %s", r.status, r.body)
	}
	if columns, spacing := gridSettings(t, r.body); columns != "5" || spacing != "compact" {
		t.Fatalf("the grid answered has columns %s, spacing %q", columns, spacing)
	}
	if !strings.Contains(r.body, "--lm-cols:5") {
		t.Fatalf("the grid answered lays out %.300q", r.body)
	}
	if got := spans(r.body); got["one"] != 1 || got["full"] != 5 || got["two"] != 2 {
		t.Fatalf("spans after 3 → 5 columns: %v, want one 1, full 5, two 2", got)
	}
	draft := c.draft(slug).state.Layouts
	if draft[0].Columns != 5 || draft[0].Spacing != layoutmgr.SpacingCompact {
		t.Fatalf("the draft's page 1 has columns %d, spacing %q", draft[0].Columns, draft[0].Spacing)
	}
	if draft[1].Columns != 0 || draft[1].Spacing != "" || draft[1].Blocks[0].Cols != 3 {
		t.Fatalf("the draft's page 2 changed too: %+v", draft[1])
	}
	if cm := c.committed(slug).state.Layouts[0]; cm.Columns != 0 || cm.Spacing != "" {
		t.Fatalf("committed before Done: %+v", cm)
	}

	// Fewer columns: the wide block fills the page, the one wider than the
	// page is cut down to it.
	r = c.postForm(settings("0", url.Values{"columns": {"2"}}), nil)
	if got := spans(r.body); r.status != http.StatusOK || got["one"] != 1 || got["full"] != 2 || got["two"] != 2 {
		t.Fatalf("5 → 2 columns: %d, spans %v", r.status, got)
	}
	if _, spacing := gridSettings(t, r.body); spacing != "compact" {
		t.Fatalf("a change of columns alone changed the spacing to %q", spacing)
	}

	if r := c.postForm(lm(slug, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusNoContent {
		t.Fatalf("Done: %d %s", r.status, r.body)
	}
	cm := c.committed(slug).state.Layouts[0]
	if cm.Columns != 2 || cm.Spacing != layoutmgr.SpacingCompact {
		t.Fatalf("committed page 1: columns %d, spacing %q", cm.Columns, cm.Spacing)
	}
	// View mode shows them.
	body := c.page(slug).body
	if columns, spacing := gridSettings(t, body); columns != "2" || spacing != "compact" {
		t.Fatalf("view mode shows columns %s, spacing %q", columns, spacing)
	}

	// GridColumns and an empty spacing are the page's own: kept as no choice,
	// so the page follows the application's settings if they change.
	if r := c.postForm(settings("0", url.Values{"columns": {"3"}, "spacing": {""}}), nil); r.status != http.StatusOK {
		t.Fatalf("settings back to the defaults: %d %s", r.status, r.body)
	}
	if l := c.draft(slug).state.Layouts[0]; l.Columns != 0 || l.Spacing != "" {
		t.Fatalf("the defaults are stored as columns %d, spacing %q", l.Columns, l.Spacing)
	}

	// Reset drops the draft, settings with the rest.
	r = c.postForm(lm(slug, "reset", "layout=0"), nil)
	if columns, spacing := gridSettings(t, r.body); r.status != http.StatusOK || columns != "2" || spacing != "compact" {
		t.Fatalf("Reset: %d, columns %s, spacing %q", r.status, columns, spacing)
	}
}

// A block resized on a page with columns of its own is resized within them.
func TestResizeFollowsThePagesColumns(t *testing.T) {
	reset(t)
	const slug = e2eapp.PageSettings
	c := newClient(t)
	id := c.addBlock(slug, 0, "short")
	if r := c.postForm(settings("0", url.Values{"columns": {"5"}}), nil); r.status != http.StatusOK {
		t.Fatalf("settings: %d %s", r.status, r.body)
	}
	resize := func(query string) string {
		t.Helper()
		r := c.postForm(lm(slug, "resize", "id="+id, "layout=0", query), nil)
		if r.status != http.StatusNoContent {
			t.Fatalf("resize %s: %d %s", query, r.status, r.body)
		}
		return r.header.Get("X-Layout-Manager-Cols")
	}
	if got := resize("cols=0"); got != "5" {
		t.Fatalf("full width on a 5-column page: %s", got)
	}
	if got := resize("delta=1"); got != "5" {
		t.Fatalf("growing past the page: %s", got)
	}
	if got := resize("cols=9"); got != "5" {
		t.Fatalf("a span wider than the page: %s", got)
	}
}

// The settings take only what the dialog offers.
func TestSettingsRefuseWhatTheDialogDoesNotOffer(t *testing.T) {
	reset(t)
	const slug = e2eapp.PageSettings
	c := newClient(t)
	for _, form := range []url.Values{
		{"columns": {"0"}},
		{"columns": {"7"}}, // MaxColumns is 6 by default
		{"columns": {"three"}},
		{"spacing": {"roomy"}},
	} {
		if r := c.postForm(settings("0", form), nil); r.status != http.StatusBadRequest {
			t.Errorf("settings %v: %d, want 400", form, r.status)
		}
	}
	if r := c.postForm(settings("3", url.Values{"columns": {"2"}}), nil); r.status != http.StatusBadRequest {
		t.Errorf("settings of page 4 of 3: %d, want 400", r.status)
	}
	if r := c.get(settings("0", url.Values{"columns": {"2"}})); r.status != http.StatusMethodNotAllowed {
		t.Errorf("GET settings: %d, want 405", r.status)
	}
	if l := c.draft(slug).state.Layouts; len(l) > 0 && (l[0].Columns != 0 || l[0].Spacing != "") {
		t.Fatalf("a refused request changed the draft: %+v", l[0])
	}
	// Every column the dialog offers, and no more.
	body := c.page(slug).body
	if n := strings.Count(body, `name="lm-columns"`); n != 6 {
		t.Fatalf("the dialog offers %d column counts, want 6", n)
	}
	if n := strings.Count(body, `name="lm-spacing"`); n != 4 {
		t.Fatalf("the dialog offers %d spacings, want 4", n)
	}
}

// Without Settings there is no dialog and no route, and a layout stored with
// settings shows GridColumns and the vertical padding's spacing.
func TestSettingsAreOffUnlessThePageAsksForThem(t *testing.T) {
	reset(t)
	c := newClient(t)
	c.plant(page, layoutmgr.LayoutState{Layouts: []layoutmgr.Layout{
		{Columns: 5, Spacing: layoutmgr.SpacingNone, Blocks: []layoutmgr.Block{{ID: "wide", Type: "short", Cols: 5}}},
	}})
	body := c.page(page).body
	if strings.Contains(body, "data-lm-settings") {
		t.Fatal("a page without Settings renders the settings")
	}
	if columns, spacing := gridSettings(t, body); columns != "3" || spacing != "" {
		t.Fatalf("a stored choice shows without Settings: columns %s, spacing %q", columns, spacing)
	}
	if got := spans(body)["wide"]; got != 3 {
		t.Fatalf("a block stored 5 wide shows %d wide on a 3-column page", got)
	}
	if r := c.postForm(lm(page, "settings", "layout=0", "columns=2"), nil); r.status != http.StatusBadRequest {
		t.Fatalf("settings on a page without them: %d, want 400", r.status)
	}
}

// The page root says what the stylesheet lays out by: the settings' URL, and
// where the toolbar of a page with no title goes.
func TestPageRootCarriesTheSettingsAndToolbarAlignment(t *testing.T) {
	reset(t)
	c := newClient(t)
	cases := []struct {
		slug         string
		settings     bool
		alignedRight bool
		titled       bool
	}{
		{e2eapp.PageSettings, true, false, true},
		{page, false, false, true},
		{e2eapp.PageUntitled, false, false, false},
		{e2eapp.PageRight, false, true, false},
	}
	for _, tc := range cases {
		body := c.page(tc.slug).body
		root := pageRoot.FindString(body)
		if got := strings.Contains(root, "data-lm-settings-url="); got != tc.settings {
			t.Errorf("%s: settings URL on the root %v, want %v", tc.slug, got, tc.settings)
		}
		if got := strings.Contains(root, `data-lm-toolbar-align="right"`); got != tc.alignedRight {
			t.Errorf("%s: toolbar aligned right %v, want %v", tc.slug, got, tc.alignedRight)
		}
		if got := strings.Contains(body, "ir-panel-page-title"); got != tc.titled {
			t.Errorf("%s: titled %v, want %v", tc.slug, got, tc.titled)
		}
	}
}
