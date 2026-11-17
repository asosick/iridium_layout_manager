package layoutmgr_test

// Regression tests for the defects TESTING_PLAN.md suspected (and S17, found
// while testing), checked through the real routes.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

// S3: two requests the browser sends before either answers both apply, one
// after the other, even when they carry the same cookies.
func TestS3_RequestsSentTogetherBothApply(t *testing.T) {
	reset(t)

	t.Run("two adds", func(t *testing.T) {
		c := newClient(t)
		first := c.addBlock(page, 0, "short")
		cookies := c.snapshot()
		form := url.Values{"type": {"tall"}, "layout": {"0"}}.Encode()
		r1 := c.sendWith(cookies, c.request(http.MethodPost, lm(page, "add"), strings.NewReader(form), "application/x-www-form-urlencoded"))
		r2 := c.sendWith(cookies, c.request(http.MethodPost, lm(page, "add"), strings.NewReader(form), "application/x-www-form-urlencoded"))
		if r1.status != http.StatusOK || r2.status != http.StatusOK {
			t.Fatalf("adds answered %d and %d", r1.status, r2.status)
		}
		if ids := c.draft(page).ids(0); len(ids) != 3 || ids[0] != first {
			t.Fatalf("after two quick adds the layout holds %v, want %s and two new blocks", ids, first)
		}
	})

	t.Run("remove, then a drag sent before the remove answered", func(t *testing.T) {
		c := newClient(t)
		a := c.addBlock(page, 0, "short")
		b := c.addBlock(page, 0, "short")
		d := c.addBlock(page, 0, "short")
		cookies := c.snapshot()
		r1 := c.sendWith(cookies, c.request(http.MethodPost, lm(page, "remove", "id="+a, "layout=0"), nil, ""))
		// The drag's order still lists a: the remove had not answered.
		r2 := c.sendWith(cookies, c.request(http.MethodPost, lm(page, "reorder", "layout=0"),
			strings.NewReader(url.Values{"order": {d, b, a}}.Encode()), "application/x-www-form-urlencoded"))
		if r1.status != http.StatusNoContent || r2.status != http.StatusNoContent {
			t.Fatalf("remove %d, reorder %d", r1.status, r2.status)
		}
		if ids := c.draft(page).ids(0); !slices.Equal(ids, []string{d, b}) {
			t.Fatalf("layout %v, want the drag's order without the removed block", ids)
		}
	})
}

// S4: layouts no longer live in a 4 KB cookie: a page takes a hundred
// blocks, and other pages are unaffected.
func TestS4_NoCeilingOnTheNumberOfBlocks(t *testing.T) {
	reset(t)
	c := newClient(t)
	for n := 1; n <= 100; n++ {
		if r := c.add(page, 0, "short"); r.status != http.StatusOK {
			t.Fatalf("block %d of 100: %d %q", n, r.status, strings.TrimSpace(r.body))
		}
	}
	if ids := c.draft(page).ids(0); len(ids) != 100 {
		t.Fatalf("the layout holds %d blocks, want 100", len(ids))
	}
	if r := c.postForm(lm(page, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusNoContent {
		t.Fatalf("Done with 100 blocks: %d", r.status)
	}
	if r := c.add(e2eapp.PageSingle, 0, "short"); r.status != http.StatusOK {
		t.Fatalf("another page's first block: %d", r.status)
	}
	for _, ck := range c.snapshot() {
		if len(ck.Value) > 512 {
			t.Fatalf("cookie %s carries %d bytes", ck.Name, len(ck.Value))
		}
	}
}

// S10: a layout index the page does not have is refused, not read as page 1.
func TestS10_AnInvalidLayoutIndexIsRefused(t *testing.T) {
	reset(t)
	for _, layout := range []string{"7", "-1", "two"} {
		t.Run("layout="+layout, func(t *testing.T) {
			c := newClient(t)
			r := c.postForm(lm(page, "add"), url.Values{"type": {"short"}, "layout": {layout}})
			if r.status != http.StatusBadRequest {
				t.Errorf("add to layout %q of 3 answered %d, want 400", layout, r.status)
			}
			if ids := c.draft(page).ids(0); len(ids) != 0 {
				t.Errorf("page 1 gained %d block(s)", len(ids))
			}
		})
	}
}

// S17: over plain HTTP on a LAN host name, layouts persist: for a signed-in
// user they are keyed by the user, and a visitor who is not signed in gets a
// key cookie with Iridium's cookie options (not Secure over HTTP).
func TestS17_LayoutsPersistOverPlainHTTP(t *testing.T) {
	reset(t)
	_, plain := servers(t)
	lan := "http://dashboard.lan:" + plain.URL[strings.LastIndex(plain.URL, ":")+1:]

	for name, open := range map[string]func(*testing.T, *httptest.Server, string) *client{
		"signed in":     newClientAt,
		"not signed in": newAnonymousClientAt,
	} {
		t.Run(name, func(t *testing.T) {
			c := open(t, plain, lan)
			id := c.addBlock(page, 0, "short")
			if r := c.postForm(lm(page, "save"), url.Values{"layout": {"0"}}); r.status != http.StatusNoContent {
				t.Fatalf("Done: %d", r.status)
			}
			if ids := gridIDs(c.page(page).body); !slices.Equal(ids, []string{id}) {
				t.Fatalf("after a reload the page shows %v, want [%s]", ids, id)
			}
			for _, line := range c.firstPage.header.Values("Set-Cookie") {
				if strings.HasPrefix(line, "layout-manager-key=") {
					if strings.Contains(line, "Secure") || !strings.Contains(line, "HttpOnly") {
						t.Fatalf("visitor key cookie over plain HTTP: %s", line)
					}
				}
			}
		})
	}
}
