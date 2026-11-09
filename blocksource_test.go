package layoutmgr

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// Blocks from BlockSource are offered after the static ones, can be added,
// and render; one the source stops returning renders as missing.
func TestBlockSourceAddsBlocksPerRequest(t *testing.T) {
	offered := []string{"cam-1", "cam-2"}
	p := NewLayoutManagerPage("Wall", "wall").
		Blocks(StaticBlock("note", "Note", templ.Raw("<p>note</p>"))).
		BlockSource(func(*http.Request) []BlockSpec {
			var out []BlockSpec
			for _, key := range offered {
				out = append(out, StaticBlock(key, strings.ToUpper(key), templ.Raw("<p>"+key+"</p>")))
			}
			return out
		})
	var state LayoutState
	p.sessionLoad = func(*http.Request) (LayoutState, error) { return state, nil }
	p.sessionExists = func(*http.Request) (bool, error) { return true, nil }
	p.sessionSave = func(_ *http.Request, s LayoutState) error { state = s; return nil }
	p.committedExists = func(*http.Request) (bool, error) { return true, nil }

	r := httptest.NewRequest(http.MethodGet, "/wall", nil)
	d := p.buildPageData(httptest.NewRecorder(), r, p.currentState(r), 0)
	var keys []string
	for _, o := range d.AddOptions {
		keys = append(keys, o.Key)
	}
	if strings.Join(keys, ",") != "note,cam-1,cam-2" {
		t.Fatalf("offered %v", keys)
	}

	form := url.Values{"type": {"cam-2"}, "layout": {"0"}}
	add := httptest.NewRequest(http.MethodPost, "/wall/lm/add", strings.NewReader(form.Encode()))
	add.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	p.handleAdd(w, add)
	if w.Code != http.StatusOK {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	if len(state.Layouts) == 0 || len(state.Layouts[0].Blocks) != 1 || state.Layouts[0].Blocks[0].Type != "cam-2" {
		t.Fatalf("state %+v", state)
	}

	render := func() string {
		d := p.buildPageData(httptest.NewRecorder(), r, p.currentState(r), 0)
		var b bytes.Buffer
		if err := d.Blocks[0].Renderer.Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if got := render(); !strings.Contains(got, "<p>cam-2</p>") {
		t.Fatalf("rendered %q", got)
	}
	offered = []string{"cam-1"}
	if got := render(); strings.Contains(got, "cam-2</p>") || !strings.Contains(got, "cam-2") {
		t.Fatalf("a block the source dropped rendered %q, want the missing placeholder", got)
	}
}
