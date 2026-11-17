package layoutmgr_test

// Suspected defects from TESTING_PLAN.md, checked through the real routes.
// Each check states the behaviour the plugin should have; knownDefect keeps
// the suite green while the defect reproduces and fails once it is fixed.

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

// S3: every mutation reads the layout from the request's cookie and writes
// the whole layout back. Two requests the browser sends before either
// answer carry the same cookie, so the one that lands last silently undoes
// the other. Nothing serialises them on the client or the server.
func TestS3_RequestsSentTogetherOverwriteEachOther(t *testing.T) {
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
		knownDefect(t, "S3", func(c2 *checker) {
			ids := c.draft(page).ids(0)
			if len(ids) != 3 {
				c2.Fatalf("after two quick adds the layout holds %d blocks %v, want 3 (%s and two new)", len(ids), ids, first)
			}
		})
	})

	t.Run("remove, then a drag before the remove answered", func(t *testing.T) {
		c := newClient(t)
		a := c.addBlock(page, 0, "short")
		b := c.addBlock(page, 0, "short")
		d := c.addBlock(page, 0, "short")
		cookies := c.snapshot()
		r1 := c.sendWith(cookies, c.request(http.MethodPost, lm(page, "remove", "id="+a, "layout=0"), nil, ""))
		// The drag's order still lists a: the remove's swap had not happened.
		r2 := c.sendWith(cookies, c.request(http.MethodPost, lm(page, "reorder", "layout=0"),
			strings.NewReader(`{"order":["`+d+`","`+b+`","`+a+`"]}`), "application/json"))
		if r1.status != http.StatusOK || r2.status != http.StatusOK {
			t.Fatalf("remove %d, reorder %d", r1.status, r2.status)
		}
		knownDefect(t, "S3", func(c2 *checker) {
			if ids := c.draft(page).ids(0); slices.Contains(ids, a) {
				c2.Fatalf("the removed block came back: %v", ids)
			}
		})
	})
}

// S4: the draft and the committed snapshot each live in one gorilla cookie
// (4096 bytes encoded), shared by every layout-manager page of the app and
// all of their layouts. Past the ceiling every edit answers 500.
func TestS4_LayoutsHitTheCookieCeiling(t *testing.T) {
	reset(t)
	c := newClient(t)
	cookieSize := func() int {
		for _, ck := range c.snapshot() {
			if ck.Name == "layout-manager" {
				return len(ck.Value)
			}
		}
		return 0
	}

	const want = 100
	placed, size := 0, 0
	var failure response
	for placed < want {
		r := c.add(page, 0, "short")
		if r.status != http.StatusOK {
			failure = r
			break
		}
		placed++
		size = cookieSize()
	}
	t.Logf("cookie ceiling: %d blocks fit (draft cookie %d bytes, ~%d bytes per block); block %d answered %d %q",
		placed, size, size/max(placed, 1), placed+1, failure.status, strings.TrimSpace(failure.body))

	knownDefect(t, "S4", func(c2 *checker) {
		if placed < want {
			c2.Fatalf("only %d of %d blocks could be placed: the next add answered %d", placed, want, failure.status)
		}
	})
	if placed == want {
		return
	}

	// The failed edit leaves the stored draft as it was...
	if ids := c.draft(page).ids(0); len(ids) != placed {
		t.Fatalf("after the failed add the draft holds %d blocks, want %d", len(ids), placed)
	}
	if failure.status != http.StatusInternalServerError {
		t.Fatalf("the add past the ceiling answered %d, want the 500 the handler sends", failure.status)
	}
	// ...and the ceiling is per app, not per page: an empty page of another
	// layout manager cannot take a single block either.
	knownDefect(t, "S4", func(c2 *checker) {
		if r := c.add(e2eapp.PageSingle, 0, "short"); r.status != http.StatusOK {
			c2.Fatalf("an empty page refused its first block (%d): every page shares the one cookie", r.status)
		}
	})
}

// S10: an out-of-range or non-numeric layout index is read as 0, so a stale
// or wrong request edits page 1 instead of failing.
func TestS10_AnInvalidLayoutIndexEditsPageOne(t *testing.T) {
	reset(t)
	for _, layout := range []string{"7", "-1", "two"} {
		t.Run("layout="+layout, func(t *testing.T) {
			c := newClient(t)
			r := c.postForm(lm(page, "add"), url.Values{"type": {"short"}, "layout": {layout}})
			knownDefect(t, "S10", func(c2 *checker) {
				if r.status != http.StatusBadRequest {
					c2.Errorf("add to layout %q of 3 answered %d, want 400", layout, r.status)
				}
				if ids := c.draft(page).ids(0); len(ids) != 0 {
					c2.Errorf("page 1 gained %d block(s)", len(ids))
				}
			})
		})
	}
}

// S17 (found while building the harness): the plugin's cookies take gorilla's
// defaults (Secure, SameSite=None, 30 days) instead of Iridium's cookie
// options, which drop Secure on plain HTTP. Over plain HTTP the client never
// sends them back, so every edit is lost on the next request.
func TestS17_LayoutsDoNotPersistOverPlainHTTP(t *testing.T) {
	reset(t)
	_, plain := servers(t)
	// A dashboard served over plain HTTP on the LAN.
	c := newClientAt(t, plain, "http://dashboard.lan:"+plain.URL[strings.LastIndex(plain.URL, ":")+1:])

	r := c.add(page, 0, "short")
	if r.status != http.StatusOK || len(gridIDs(r.body)) != 1 {
		t.Fatalf("add: %d, grid %v", r.status, gridIDs(r.body))
	}
	var layoutCookie, csrfCookie string
	for _, line := range r.header.Values("Set-Cookie") {
		if strings.HasPrefix(line, "layout-manager=") {
			layoutCookie = line[strings.Index(line, ";"):]
		}
	}
	for _, line := range c.firstPage.header.Values("Set-Cookie") {
		if strings.HasPrefix(line, "ir-csrf=") {
			csrfCookie = line[strings.Index(line, ";"):]
		}
	}
	t.Logf("over plain HTTP the layout cookie is set with %q (Iridium's own: %q)", layoutCookie, csrfCookie)

	knownDefect(t, "S17", func(c2 *checker) {
		if ids := gridIDs(c.page(page).body); len(ids) != 1 {
			c2.Fatalf("over plain HTTP the added block is gone on the next page load (%d blocks)", len(ids))
		}
	})
}
