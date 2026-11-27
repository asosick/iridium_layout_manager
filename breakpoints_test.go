package layoutmgr_test

// Columns per breakpoint (GridColumns' map, and "Columns per screen size" in
// the settings dialog), through the real panel.

import (
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	layoutmgr "github.com/asosick/iridium_layout_manager"
	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

var columnsAtAttr = regexp.MustCompile(`data-lm-columns-at="([\d,]+)"`)

// columnsAt reads a rendered grid's columns at each breakpoint, xs first.
func columnsAt(t *testing.T, body string) string {
	t.Helper()
	m := columnsAtAttr.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no columns per breakpoint in %.300q", body)
	}
	return m[1]
}

func responsiveSettings(form url.Values) string {
	return lm(e2eapp.PageResponsive, "settings", "layout=0") + "&" + form.Encode()
}

// GridColumns' map cascades from xs up, as Iridium's column maps do, and the
// grid carries every breakpoint's count for the stylesheet.
func TestGridColumnsPerBreakpoint(t *testing.T) {
	reset(t)
	c := newClient(t)
	body := c.page(e2eapp.PageResponsive).body
	if got := columnsAt(t, body); got != "1,1,2,4,4,4" {
		t.Fatalf("columns per breakpoint %s, want 1,1,2,4,4,4", got)
	}
	const style = "--lm-cols-xs:1;--lm-cols-sm:1;--lm-cols-md:2;--lm-cols-lg:4;--lm-cols-xl:4;--lm-cols-2xl:4;--lm-cols-max:4"
	if !strings.Contains(body, style) {
		t.Fatalf("the grid's style is not %s in %.400q", style, body)
	}
	if columns, _ := gridSettings(t, body); columns != "4" {
		t.Fatalf("the page's widest is %s columns, want 4", columns)
	}
	if strings.Contains(body, "data-lm-columns-custom") {
		t.Fatal("GridColumns' own breakpoints show as chosen by the user")
	}
	// The dialog has a choice of columns for every breakpoint.
	for _, bp := range []string{"xs", "sm", "md", "lg", "xl", "2xl"} {
		if !strings.Contains(body, `name="lm-columns-`+bp+`"`) {
			t.Errorf("the dialog has no columns for %s", bp)
		}
	}
}

// Choosing the page's columns alone changes it at its widest and keeps its
// smaller screens, up to the number chosen.
func TestSettingsColumnsKeepTheSmallerScreens(t *testing.T) {
	reset(t)
	const slug = e2eapp.PageResponsive
	c := newClient(t)
	c.plant(slug, layoutmgr.LayoutState{Layouts: []layoutmgr.Layout{{Blocks: []layoutmgr.Block{
		{ID: "one", Type: "short", Cols: 1},
		{ID: "full", Type: "short", Cols: 4},
	}}}})

	r := c.postForm(responsiveSettings(url.Values{"columns": {"6"}}), nil)
	if r.status != http.StatusOK {
		t.Fatalf("settings: %d %s", r.status, r.body)
	}
	if got := columnsAt(t, r.body); got != "1,1,2,6,6,6" {
		t.Fatalf("6 columns gives %s, want 1,1,2,6,6,6", got)
	}
	if got := spans(r.body); got["one"] != 1 || got["full"] != 6 {
		t.Fatalf("spans after 4 → 6 columns: %v", got)
	}
	l := c.draft(slug).state.Layouts[0]
	if l.Columns != 6 || l.ColumnBreakpoints != nil {
		t.Fatalf("stored columns %d, breakpoints %v: want 6 alone", l.Columns, l.ColumnBreakpoints)
	}

	// Fewer than a smaller screen has: that screen gets no more than chosen.
	r = c.postForm(responsiveSettings(url.Values{"columns": {"1"}}), nil)
	if got := columnsAt(t, r.body); got != "1,1,1,1,1,1" {
		t.Fatalf("1 column gives %s", got)
	}
}

// Columns chosen breakpoint by breakpoint are kept as they are, cascading up
// as GridColumns' do, and the dialog opens on them.
func TestSettingsColumnsPerBreakpoint(t *testing.T) {
	reset(t)
	const slug = e2eapp.PageResponsive
	c := newClient(t)
	c.plant(slug, layoutmgr.LayoutState{Layouts: []layoutmgr.Layout{{Blocks: []layoutmgr.Block{
		{ID: "full", Type: "short", Cols: 4},
	}}}})

	r := c.postForm(responsiveSettings(url.Values{
		"columns_xs": {"2"}, "columns_md": {"3"}, "columns_xl": {"5"},
	}), nil)
	if r.status != http.StatusOK {
		t.Fatalf("settings: %d %s", r.status, r.body)
	}
	if got := columnsAt(t, r.body); got != "2,2,3,3,5,5" {
		t.Fatalf("columns per breakpoint %s, want 2,2,3,3,5,5", got)
	}
	if !strings.Contains(r.body, "data-lm-columns-custom") {
		t.Fatal("columns chosen breakpoint by breakpoint are not marked as such")
	}
	if got := spans(r.body)["full"]; got != 5 {
		t.Fatalf("a full-width block is %d wide on a page 5 at its widest", got)
	}
	l := c.draft(slug).state.Layouts[0]
	want := map[string]int{"xs": 2, "sm": 2, "md": 3, "lg": 3, "xl": 5, "2xl": 5}
	if l.Columns != 5 || !maps.Equal(l.ColumnBreakpoints, want) {
		t.Fatalf("stored columns %d, breakpoints %v", l.Columns, l.ColumnBreakpoints)
	}

	// A choice of columns alone then keeps these breakpoints' shape.
	r = c.postForm(responsiveSettings(url.Values{"columns": {"4"}}), nil)
	if got := columnsAt(t, r.body); got != "2,2,3,3,4,4" {
		t.Fatalf("4 columns after per-breakpoint columns gives %s", got)
	}

	// GridColumns' breakpoints again: nothing chosen, so the page follows
	// GridColumns.
	r = c.postForm(responsiveSettings(url.Values{
		"columns_xs": {"1"}, "columns_md": {"2"}, "columns_lg": {"4"},
	}), nil)
	if got := columnsAt(t, r.body); got != "1,1,2,4,4,4" {
		t.Fatalf("GridColumns' breakpoints give %s", got)
	}
	if l := c.draft(slug).state.Layouts[0]; l.Columns != 0 || l.ColumnBreakpoints != nil {
		t.Fatalf("GridColumns' breakpoints are stored as columns %d, breakpoints %v", l.Columns, l.ColumnBreakpoints)
	}
	if strings.Contains(r.body, "data-lm-columns-custom") {
		t.Fatal("GridColumns' breakpoints show as chosen by the user")
	}
}

// Every breakpoint takes only the columns the dialog offers.
func TestSettingsRefuseBreakpointColumnsTheDialogDoesNotOffer(t *testing.T) {
	reset(t)
	c := newClient(t)
	for _, form := range []url.Values{
		{"columns_md": {"0"}},
		{"columns_lg": {"7"}}, // MaxColumns is 6 by default
		{"columns_xs": {"two"}},
	} {
		if r := c.postForm(responsiveSettings(form), nil); r.status != http.StatusBadRequest {
			t.Errorf("settings %v: %d, want 400", form, r.status)
		}
	}
	if l := c.draft(e2eapp.PageResponsive).state.Layouts; len(l) > 0 && (l[0].Columns != 0 || l[0].ColumnBreakpoints != nil) {
		t.Fatalf("a refused request changed the draft: %+v", l[0])
	}
}

// Without Settings, columns stored per breakpoint are ignored.
func TestStoredBreakpointColumnsNeedSettings(t *testing.T) {
	reset(t)
	c := newClient(t)
	c.plant(page, layoutmgr.LayoutState{Layouts: []layoutmgr.Layout{
		{Columns: 5, ColumnBreakpoints: map[string]int{"xs": 5}},
	}})
	if got := columnsAt(t, c.page(page).body); got != "1,3,3,3,3,3" {
		t.Fatalf("a page without Settings has %s, want GridColumns' 1,3,3,3,3,3", got)
	}
}

// The stored shape of columns per breakpoint, for a SaveHook.
func TestBreakpointColumnsRoundTripAsJSON(t *testing.T) {
	reset(t)
	const slug = e2eapp.PageResponsive
	c := newClient(t)
	c.addBlock(slug, 0, "short")
	if r := c.postForm(responsiveSettings(url.Values{"columns_xs": {"2"}}), nil); r.status != http.StatusOK {
		t.Fatalf("settings: %d %s", r.status, r.body)
	}
	raw := c.draft(slug).raw
	if !strings.Contains(raw, `"columnBreakpoints":{`) || !strings.Contains(raw, `"xs":2`) {
		t.Fatalf("stored layout %s", raw)
	}
	if r := c.postForm(lm(slug, "save"), nil); r.status >= http.StatusBadRequest {
		t.Fatalf("save: %d", r.status)
	}
	if got := columnsAt(t, c.page(slug).body); got != "2,2,2,2,2,2" {
		t.Fatalf("view mode after Done shows %s", got)
	}
}
