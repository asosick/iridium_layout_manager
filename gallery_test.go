package layoutmgr_test

// The block gallery's server side: blocks placed at a position (a card
// dropped between two blocks) and the previews its cards show.

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	layoutmgr "github.com/asosick/iridium_layout_manager"
	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

func TestInsertPutsTheBlockAtTheIndexOrAfterTheLast(t *testing.T) {
	ids := func(l layoutmgr.Layout) []string {
		var out []string
		for _, b := range l.Blocks {
			out = append(out, b.ID)
		}
		return out
	}
	l := layoutmgr.Layout{}
	for _, tc := range []struct {
		id   string
		at   int
		want []string
		took int
	}{
		{"a", -1, []string{"a"}, 0},
		{"b", 0, []string{"b", "a"}, 0},
		{"c", 1, []string{"b", "c", "a"}, 1},
		{"d", 3, []string{"b", "c", "a", "d"}, 3},
		{"e", 99, []string{"b", "c", "a", "d", "e"}, 4},
	} {
		if took := l.Insert(layoutmgr.Block{ID: tc.id}, tc.at); took != tc.took {
			t.Fatalf("insert %s at %d took %d, want %d", tc.id, tc.at, took, tc.took)
		}
		if got := ids(l); !slices.Equal(got, tc.want) {
			t.Fatalf("after %s at %d: %v, want %v", tc.id, tc.at, got, tc.want)
		}
	}
}

// A block dropped between two others is stored there, and answered alone.
func TestAddAtAnIndexPlacesTheBlockThere(t *testing.T) {
	reset(t)
	c := newClient(t)
	a := c.addBlock(page, 0, "short")
	b := c.addBlock(page, 0, "tall")

	r := c.postForm(lm(page, "add"), url.Values{"type": {"grow"}, "layout": {"0"}, "at": {"1"}})
	if r.status != http.StatusOK {
		t.Fatalf("add at 1: %d %s", r.status, r.body)
	}
	draft := c.draft(page).layout(0)
	if len(draft) != 3 || draft[0].ID != a || draft[2].ID != b || draft[1].Type != "grow" {
		t.Fatalf("draft after add at 1: %+v", draft)
	}
	if got := gridIDs(r.body); !slices.Equal(got, []string{draft[1].ID}) {
		t.Fatalf("answered blocks %v, want only the new one", got)
	}
	// It can move either way: it is neither first nor last. (Not to be
	// confused with Alpine's :disabled binding.)
	if disabledAttr.MatchString(r.body) {
		t.Fatal("a block placed in the middle answered with a move disabled")
	}

	if r := c.postForm(lm(page, "add"), url.Values{"type": {"short"}, "layout": {"0"}, "at": {"0"}}); r.status != http.StatusOK {
		t.Fatalf("add at 0: %d", r.status)
	}
	if first := c.draft(page).layout(0)[0]; first.Type != "short" || first.ID == a {
		t.Fatalf("add at 0 stored %+v first", first)
	}
	if r := c.postForm(lm(page, "add"), url.Values{"type": {"slow"}, "layout": {"0"}, "at": {"40"}}); r.status != http.StatusOK {
		t.Fatalf("add past the end: %d", r.status)
	}
	if last := c.draft(page).layout(0)[4]; last.Type != "slow" {
		t.Fatalf("add past the end stored %+v last", last)
	}

	before := c.draft(page).ids(0)
	for _, at := range []string{"-1", "two", "1.5"} {
		if r := c.postForm(lm(page, "add"), url.Values{"type": {"short"}, "layout": {"0"}, "at": {at}}); r.status != http.StatusBadRequest {
			t.Fatalf("add at %q: %d, want 400", at, r.status)
		}
	}
	if got := c.draft(page).ids(0); !slices.Equal(got, before) {
		t.Fatalf("refused adds changed the draft: %v, was %v", got, before)
	}
}

var disabledAttr = regexp.MustCompile(`\sdisabled[\s=>]`)

var previewID = regexp.MustCompile(`data-e2e-id="(lm-preview-[0-9a-z]+)"`)

// A card's preview is the block itself, told it is a preview and given an
// id of its own; showing it changes nothing.
func TestPreviewRendersTheBlockAndChangesNothing(t *testing.T) {
	reset(t)
	c := newClient(t)
	placed := c.addBlock(page, 0, "short")

	r := c.get(lm(page, "preview", "type=short"))
	if r.status != http.StatusOK {
		t.Fatalf("preview: %d %s", r.status, r.body)
	}
	if !strings.Contains(r.body, `data-e2e-kind="short"`) || !strings.Contains(r.body, `data-e2e-preview="true"`) {
		t.Fatalf("preview body: %s", r.body)
	}
	m := previewID.FindStringSubmatch(r.body)
	if m == nil || m[1] == placed {
		t.Fatalf("the preview's id is not a preview id of its own: %s", r.body)
	}
	if again := previewID.FindStringSubmatch(c.get(lm(page, "preview", "type=short")).body); again == nil || again[1] != m[1] {
		t.Fatalf("a block type's preview id changed between renders")
	}
	if other := previewID.FindStringSubmatch(c.get(lm(page, "preview", "type=tall")).body); other == nil || other[1] == m[1] {
		t.Fatalf("two block types share a preview id")
	}
	if strings.Contains(r.body, "lm-block-controls") {
		t.Fatal("a preview carries a placed block's controls")
	}
	if got := c.draft(page).ids(0); !slices.Equal(got, []string{placed}) {
		t.Fatalf("a preview changed the draft: %v", got)
	}
	// The placed block still renders as placed.
	if body := c.editGrid(page, 0).body; !strings.Contains(body, `data-e2e-preview="false"`) {
		t.Fatal("a placed block renders as a preview")
	}

	// Real widgets preview too.
	if r := c.get(lm(page, "preview", "type=table")); r.status != http.StatusOK || !strings.Contains(r.body, "#1001") {
		t.Fatalf("table preview: %d", r.status)
	}

	if r := c.get(lm(page, "preview", "type=nope")); r.status != http.StatusNotFound {
		t.Fatalf("unknown type: %d, want 404", r.status)
	}
	if r := c.postForm(lm(page, "preview", "type=short"), nil); r.status != http.StatusMethodNotAllowed {
		t.Fatalf("POST preview: %d, want 405", r.status)
	}
}

// A BlockSource's blocks preview like the page's own, while it offers them.
func TestSourceBlocksPreview(t *testing.T) {
	reset(t)
	c := newClient(t)
	const source = e2eapp.PageSource
	if r := c.get(lm(source, "preview", "type=cam-1")); r.status != http.StatusOK || !strings.Contains(r.body, `data-e2e-kind="cam-1"`) {
		t.Fatalf("source preview: %d %s", r.status, r.body)
	}
	setSourceKeys(t, "cam-2")
	if r := c.get(lm(source, "preview", "type=cam-1")); r.status != http.StatusNotFound {
		t.Fatalf("preview of a block the source dropped: %d, want 404", r.status)
	}
}

var cardPreview = regexp.MustCompile(`data-lm-card data-key="short" data-label="Short" data-preview-url="([^"]+)"`)

// The page offers a card per block, each with its preview's URL; a page
// shows no gallery to a user with nothing to add.
func TestTheGalleryOffersEveryBlockWithItsPreview(t *testing.T) {
	reset(t)
	c := newClient(t)
	body := c.page(page).body
	want := []string{"short", "tall", "grow", "slow", "controls", "table", "chart", "stats"}
	if got := optionValues(body); !slices.Equal(got, want) {
		t.Fatalf("cards %v, want %v", got, want)
	}
	m := cardPreview.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the short card has no preview URL")
	}
	if r := c.get(strings.ReplaceAll(m[1], "&amp;", "&")); r.status != http.StatusOK || !strings.Contains(r.body, `data-e2e-kind="short"`) {
		t.Fatalf("the card's preview URL answers %d", r.status)
	}
	if strings.Contains(body, "data-lm-type-select") {
		t.Fatal("the page still has the Add dropdown")
	}
}
