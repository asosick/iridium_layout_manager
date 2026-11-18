package layoutmgr_test

// Pages users add, name and remove themselves (the default, without
// LayoutCount), and Iridium's vertical padding, through the real panel.

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

var (
	namesAttr = regexp.MustCompile(`data-lm-names="([^"]*)"`)
	shownAttr = regexp.MustCompile(`data-lm-shown="(\d+)"`)
	pageRoot  = regexp.MustCompile(`<div[^>]*data-lm-page="true"[^>]*>`)
)

// pagesShown reads a rendered grid's pages (their names) and the page it
// shows.
func pagesShown(t *testing.T, body string) (names []string, shown int) {
	t.Helper()
	m, s := namesAttr.FindStringSubmatch(body), shownAttr.FindStringSubmatch(body)
	if m == nil || s == nil {
		t.Fatalf("no pages in %.300q", body)
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &names); err != nil {
		t.Fatal(err)
	}
	shown, _ = strconv.Atoi(s[1])
	return names, shown
}

func (s stored) names() []string {
	var out []string
	for _, l := range s.state.Layouts {
		out = append(out, l.Name)
	}
	return out
}

// Without LayoutCount the user decides how many pages there are: one to
// start with, then pages added, named and removed in the draft, which Done
// commits and Cancel drops like any other edit.
func TestUsersAddNameAndRemoveTheirOwnPages(t *testing.T) {
	reset(t)
	const pages = e2eapp.PagePages
	c := newClient(t)
	if names, _ := pagesShown(t, c.page(pages).body); !slices.Equal(names, []string{""}) {
		t.Fatalf("a new user has pages %q, want one without a name", names)
	}
	first := c.addBlock(pages, 0, "short")

	r := c.postForm(lm(pages, "pages/add"), nil)
	if names, shown := pagesShown(t, r.body); r.status != http.StatusOK || !slices.Equal(names, []string{"", ""}) || shown != 1 || len(gridIDs(r.body)) != 0 {
		t.Fatalf("add page: %d, pages %q showing %d, want the new, empty page 2", r.status, names, shown)
	}
	r = c.postForm(lm(pages, "pages/rename", "layout=1"), url.Values{"name": {"  Cameras\t North "}})
	if r.status != http.StatusOK || r.body != "Cameras North" {
		t.Fatalf("rename: %d %q, want the tidied name", r.status, r.body)
	}
	second := c.addBlock(pages, 1, "tall")

	// A draft until Done: view mode shows the one committed page.
	if names, _ := pagesShown(t, c.page(pages).body); !slices.Equal(names, []string{""}) {
		t.Fatalf("before Done view mode shows pages %q", names)
	}
	if r := c.postForm(lm(pages, "save"), url.Values{"layout": {"1"}}); r.status != http.StatusNoContent {
		t.Fatalf("Done: %d %s", r.status, r.body)
	}
	committed := c.committed(pages)
	if !slices.Equal(committed.names(), []string{"", "Cameras North"}) || !slices.Equal(committed.ids(1), []string{second}) {
		t.Fatalf("committed pages %q, page 2 holding %v", committed.names(), committed.ids(1))
	}
	if names, _ := pagesShown(t, c.page(pages).body); !slices.Equal(names, []string{"", "Cameras North"}) {
		t.Fatalf("after Done view mode shows pages %q", names)
	}

	// Cancel drops a page added since, and shows the last page there is
	// instead of it.
	if r := c.postForm(lm(pages, "pages/add"), nil); r.status != http.StatusOK {
		t.Fatalf("add page: %d", r.status)
	}
	r = c.postForm(lm(pages, "reset", "layout=2"), nil)
	if names, shown := pagesShown(t, r.body); r.status != http.StatusOK || !slices.Equal(names, []string{"", "Cameras North"}) || shown != 1 {
		t.Fatalf("Cancel: %d, pages %q showing %d", r.status, names, shown)
	}

	// Removing a page takes its blocks and shows the page before it.
	r = c.postForm(lm(pages, "pages/remove", "layout=1"), nil)
	if names, shown := pagesShown(t, r.body); r.status != http.StatusOK || !slices.Equal(names, []string{""}) || shown != 0 || !slices.Equal(gridIDs(r.body), []string{first}) {
		t.Fatalf("remove page: %d, pages %q showing %d with %v", r.status, names, shown, gridIDs(r.body))
	}
	if r := c.add(pages, 1, "short"); r.status != http.StatusBadRequest {
		t.Fatalf("an add to the removed page answered %d, want 400", r.status)
	}
	if r := c.postForm(lm(pages, "pages/remove", "layout=0"), nil); r.status != http.StatusBadRequest {
		t.Fatalf("removing the last page answered %d, want 400", r.status)
	}
	if ids := c.committed(pages).ids(1); !slices.Equal(ids, []string{second}) {
		t.Fatalf("the committed page 2 holds %v before Done", ids)
	}
}

// LayoutCount fixes the number of pages: they can be named, not added or
// removed.
func TestAFixedNumberOfPagesCanBeNamedButNotAddedOrRemoved(t *testing.T) {
	reset(t)
	c := newClient(t)
	for _, path := range []string{lm(page, "pages/add"), lm(page, "pages/remove", "layout=1")} {
		if r := c.postForm(path, nil); r.status != http.StatusBadRequest {
			t.Errorf("POST %s: %d, want 400", path, r.status)
		}
	}
	r := c.postForm(lm(page, "pages/rename", "layout=2"), url.Values{"name": {"Alarms"}})
	if r.status != http.StatusOK || r.body != "Alarms" {
		t.Fatalf("rename: %d %q", r.status, r.body)
	}
	if names := c.draft(page).names(); !slices.Equal(names, []string{"", "", "Alarms"}) {
		t.Fatalf("draft pages %q", names)
	}
	if r := c.postForm(lm(page, "pages/rename", "layout=3"), url.Values{"name": {"Nope"}}); r.status != http.StatusBadRequest {
		t.Fatalf("renaming page 4 of 3: %d, want 400", r.status)
	}
}

// Page names are part of the LayoutState the hooks store.
func TestPageNamesGoThroughTheHooks(t *testing.T) {
	reset(t)
	const hooks = e2eapp.PageHooks
	c := newClient(t)
	if r := c.postForm(lm(hooks, "pages/rename", "layout=0"), url.Values{"name": {"Overview"}}); r.status != http.StatusOK {
		t.Fatalf("rename: %d", r.status)
	}
	if r := c.postForm(lm(hooks, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusNoContent {
		t.Fatalf("Done: %d", r.status)
	}
	if db := string(hooksDoc(t).DB); !strings.Contains(db, `"name":"Overview"`) {
		t.Fatalf("the hooks stored %s", db)
	}
	other := newClient(t) // a user of their own, who reads the same store
	if names, _ := pagesShown(t, other.page(hooks).body); !slices.Equal(names, []string{"Overview", "", ""}) {
		t.Fatalf("pages read back through the LoadHook: %q", names)
	}
}

// Iridium's vertical padding reaches the page the plugin renders, whose
// stylesheet tightens the space below the header and between rows of blocks
// to match.
func TestVerticalPaddingReachesThePage(t *testing.T) {
	reset(t)
	c := newClient(t)
	for slug, want := range map[string]string{e2eapp.PageCompact: "compact", page: "default"} {
		root := pageRoot.FindString(c.page(slug).body)
		if !strings.Contains(root, `data-vertical-padding="`+want+`"`) {
			t.Errorf("%s: page root %q, want vertical padding %s", slug, root, want)
		}
	}
}
