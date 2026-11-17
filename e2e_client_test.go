package layoutmgr_test

// Integration-test plumbing: the E2E app (internal/e2eapp) served by
// httptest, and a client that behaves like a browser tab — one cookie jar,
// the CSRF token Iridium issues, and the HX-Request header htmx sends.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	layoutmgr "github.com/asosick/iridium_layout_manager"
	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

var (
	appOnce sync.Once
	app     *e2eapp.App
	tlsSrv  *httptest.Server // HTTPS, as production serves the panel
	httpSrv *httptest.Server // plain HTTP, as a LAN or dev deployment might
)

// servers starts the E2E app once per test binary (Iridium's configuration
// and session store are process-global).
func servers(t *testing.T) (*httptest.Server, *httptest.Server) {
	t.Helper()
	appOnce.Do(func() {
		app = e2eapp.New()
		tlsSrv = httptest.NewTLSServer(app.Mux)
		httpSrv = httptest.NewServer(app.Mux)
	})
	return tlsSrv, httpSrv
}

// reset restores the app's server-side state; layout state lives in each
// client's cookies.
func reset(t *testing.T) {
	t.Helper()
	srv, _ := servers(t)
	resp, err := srv.Client().Post(srv.URL+"/e2e/reset", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset: %d", resp.StatusCode)
	}
}

// client is one browser: a cookie jar and the CSRF token its session got.
type client struct {
	t    *testing.T
	base string // scheme://host:port the client addresses
	http *http.Client
	jar  *cookiejar.Jar
	csrf string
	// firstPage is the response to the page load that opened the session.
	firstPage response
}

// newClient opens a fresh browser over HTTPS and loads a page so Iridium
// issues it a CSRF token.
func newClient(t *testing.T) *client {
	t.Helper()
	srv, _ := servers(t)
	return newClientOn(t, srv)
}

func newClientOn(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	return newClientAt(t, srv, srv.URL)
}

// newClientAt opens a browser that addresses srv as base, e.g.
// http://dashboard.lan:port: cookie jars (like browsers) treat loopback
// hosts as secure origins even over HTTP, a LAN host name they do not.
func newClientAt(t *testing.T, srv *httptest.Server, base string) *client {
	t.Helper()
	return openClient(t, srv, base, false)
}

// newAnonymousClientAt opens a browser that is not signed in.
func newAnonymousClientAt(t *testing.T, srv *httptest.Server, base string) *client {
	t.Helper()
	return openClient(t, srv, base, true)
}

// openClient opens a browser. The test app signs each new browser in as a
// user of its own, so its layouts start empty.
func openClient(t *testing.T, srv *httptest.Server, base string, anonymous bool) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if anonymous {
		u, _ := url.Parse(base)
		jar.SetCookies(u, []*http.Cookie{{Name: "e2e-anonymous", Value: "1", Path: "/"}})
	}
	hc := *srv.Client()
	hc.Jar = jar
	if base != srv.URL {
		addr := srv.Listener.Addr().String()
		transport := srv.Client().Transport.(*http.Transport).Clone()
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		hc.Transport = transport
	}
	c := &client{t: t, base: base, http: &hc, jar: jar}
	c.firstPage = c.page(e2eapp.PageDefault)
	return c
}

type response struct {
	status int
	header http.Header
	body   string
}

func (c *client) do(req *http.Request) response {
	c.t.Helper()
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if token := resp.Header.Get("X-CSRF-Token"); token != "" {
		c.csrf = token
	}
	return response{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

func (c *client) request(method, path string, body io.Reader, contentType string) *http.Request {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// What htmx sends: the Spa middleware answers a fragment, not a page.
	req.Header.Set("HX-Request", "true")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	return req
}

// page loads a layout page as a full navigation (the panel shell included).
func (c *client) page(slug string) response {
	c.t.Helper()
	req := c.request(http.MethodGet, e2eapp.PanelPath+"/"+slug, nil, "")
	req.Header.Del("HX-Request")
	r := c.do(req)
	if r.status != http.StatusOK {
		c.t.Fatalf("GET %s: %d %s", slug, r.status, r.body)
	}
	return r
}

func (c *client) get(path string) response {
	c.t.Helper()
	return c.do(c.request(http.MethodGet, path, nil, ""))
}

func (c *client) postForm(path string, form url.Values) response {
	c.t.Helper()
	return c.do(c.request(http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"))
}

func (c *client) postJSON(path string, body any) response {
	c.t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		c.t.Fatal(err)
	}
	return c.do(c.request(http.MethodPost, path, strings.NewReader(string(buf)), "application/json"))
}

// lm builds the URL of one of a page's /lm/ endpoints.
func lm(slug, route string, query ...string) string {
	path := e2eapp.PanelPath + "/" + slug + "/lm/" + route
	if len(query) > 0 {
		path += "?" + strings.Join(query, "&")
	}
	return path
}

func (c *client) add(slug string, layout int, kind string) response {
	c.t.Helper()
	return c.postForm(lm(slug, "add"), url.Values{"type": {kind}, "layout": {strconv.Itoa(layout)}})
}

// addBlock adds a block and returns its ID, failing the test on any error.
// A new block is appended to its layout.
func (c *client) addBlock(slug string, layout int, kind string) string {
	c.t.Helper()
	if r := c.add(slug, layout, kind); r.status != http.StatusOK {
		c.t.Fatalf("add %s to %s/%d: %d %s", kind, slug, layout, r.status, r.body)
	}
	blocks := c.draft(slug).layout(layout)
	if len(blocks) == 0 || blocks[len(blocks)-1].Type != kind {
		c.t.Fatalf("add %s: layout %d is %+v", kind, layout, blocks)
	}
	return blocks[len(blocks)-1].ID
}

// --- stored state ----------------------------------------------------------------

type stored struct {
	state layoutmgr.LayoutState
	raw   string
}

func (s stored) layout(i int) []layoutmgr.Block {
	if i < len(s.state.Layouts) {
		return s.state.Layouts[i].Blocks
	}
	return nil
}

func (s stored) ids(i int) []string {
	var out []string
	for _, b := range s.layout(i) {
		out = append(out, b.ID)
	}
	return out
}

// stateDoc reads this client's user's layouts: the draft edit mode shows and
// the committed layout view mode shows.
func (c *client) stateDoc(slug string) (draft, committed stored) {
	c.t.Helper()
	r := c.get("/e2e/state?page=" + slug)
	if r.status != http.StatusOK {
		c.t.Fatalf("GET /e2e/state: %d %s", r.status, r.body)
	}
	var doc e2eapp.StateDoc
	if err := json.Unmarshal([]byte(r.body), &doc); err != nil {
		c.t.Fatal(err)
	}
	return c.decode(doc.Draft), c.decode(doc.Committed)
}

func (c *client) decode(raw json.RawMessage) stored {
	c.t.Helper()
	if raw == nil || string(raw) == "null" {
		return stored{}
	}
	s := stored{raw: string(raw)}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		c.t.Fatalf("decode stored layout %s: %v", raw, err)
	}
	return s
}

func (c *client) draft(slug string) stored {
	c.t.Helper()
	d, _ := c.stateDoc(slug)
	return d
}

func (c *client) committed(slug string) stored {
	c.t.Helper()
	_, cm := c.stateDoc(slug)
	return cm
}

// plant gives this client's user a committed layout, with no unsaved edits.
func (c *client) plant(slug string, saved any) {
	c.t.Helper()
	c.postState("/e2e/state?page="+slug, map[string]any{"committed": saved})
}

func (c *client) postState(path string, doc map[string]any) {
	c.t.Helper()
	if r := c.postJSON(path, doc); r.status != http.StatusNoContent {
		c.t.Fatalf("POST %s: %d %s", path, r.status, r.body)
	}
}

// editGrid loads the grid edit mode shows for one layout (the draft).
func (c *client) editGrid(slug string, layout int) response {
	c.t.Helper()
	r := c.get(lm(slug, "select", "layout="+strconv.Itoa(layout), "mode=edit"))
	if r.status != http.StatusOK {
		c.t.Fatalf("select: %d %s", r.status, r.body)
	}
	return r
}

// --- concurrency ---------------------------------------------------------------

// snapshot is the cookies a request would carry right now.
func (c *client) snapshot() []*http.Cookie {
	u, _ := url.Parse(c.base)
	return c.jar.Cookies(u)
}

// sendWith sends a request carrying exactly the given cookies, as a request
// the browser sent before an earlier response's Set-Cookie arrived, then
// stores the response's cookies in the jar the way the browser would when it
// lands.
func (c *client) sendWith(cookies []*http.Cookie, req *http.Request) response {
	c.t.Helper()
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	hc := *c.http
	hc.Jar = nil
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	u, _ := url.Parse(c.base)
	c.jar.SetCookies(u, resp.Cookies())
	return response{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

// --- rendered grid ---------------------------------------------------------------

var blockPattern = regexp.MustCompile(`data-lm-block data-id="([^"]+)" data-cols="(\d+)"`)

type renderedBlock struct {
	id   string
	cols int
}

// gridBlocks lists the blocks a rendered page or grid fragment shows.
func gridBlocks(html string) []renderedBlock {
	var out []renderedBlock
	for _, m := range blockPattern.FindAllStringSubmatch(html, -1) {
		cols, _ := strconv.Atoi(m[2])
		out = append(out, renderedBlock{id: m[1], cols: cols})
	}
	return out
}

func gridIDs(html string) []string {
	var out []string
	for _, b := range gridBlocks(html) {
		out = append(out, b.id)
	}
	return out
}
