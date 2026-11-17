package layoutmgr

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/a-h/templ"
)

// Handler behaviour is tested through the real panel in integration_test.go
// and in the browser (test/e2e). These cover the one piece no route test
// reaches: the page's own URL state handed to widgets on a page switch.

func TestHandleSelectRendersWidgetsWithCurrentPageQuery(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	state := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "table-id", Type: "table"}}}}}
	p.sessionLoad = func(_ *http.Request) (LayoutState, error) { return state, nil }
	p.sessionExists = func(_ *http.Request) (bool, error) { return true, nil }

	var renderedQuery string
	p.blocks = []BlockSpec{DynamicBlock("table", "Table", func(_ http.ResponseWriter, r *http.Request) templ.Component {
		renderedQuery = r.URL.RawQuery
		return templ.NopComponent
	})}

	r := httptest.NewRequest(http.MethodGet, "/lm/select?layout=0", nil)
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
