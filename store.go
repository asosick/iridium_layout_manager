package layoutmgr

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"

	"github.com/goiridium/iridium/bootstrap/auth"
	"github.com/goiridium/iridium/config"
	"github.com/goiridium/iridium/core/auth/user"
	"github.com/goiridium/iridium/core/context/ctxBase"
	"github.com/goiridium/iridium/core/context/ctxkeys"
	"github.com/gorilla/sessions"
)

// LoadHook fetches the committed LayoutState for the current request's user,
// for example from a database. Return a zero LayoutState{} (not an error)
// when nothing is stored yet. The plugin copies what it returns, so the hook
// may hand out the layout it stores as it is.
type LoadHook func(r *http.Request) (LayoutState, error)

// SaveHook persists the LayoutState the user commits with Done. The state is
// a copy the plugin keeps no reference to, so the hook may store it as it is.
type SaveHook func(r *http.Request, state LayoutState) error

// Layouts live on the server, per page and per user: the working draft that
// edits change, and the copy Done committed. Keeping them out of cookies
// means no size limit, and lets every request of one user take turns: two
// edits sent at once both land, in the order they arrive.
//
// The store is the process's memory. With a SaveHook and LoadHook the
// committed layout lives in the application's own storage, and only unsaved
// drafts are lost on a restart. Without hooks, layouts last as long as the
// process.

// userLayouts is one user's layouts for one page.
type userLayouts struct {
	mu sync.Mutex
	// draft holds unsaved edits; nil means the user is not editing, and the
	// draft is the committed layout.
	draft *LayoutState
	// committed is the layout Done last committed, when no hooks hold it.
	committed *LayoutState
}

type layoutStore struct {
	mu    sync.Mutex
	users map[string]*userLayouts
}

func newLayoutStore() *layoutStore {
	return &layoutStore{users: make(map[string]*userLayouts)}
}

func (s *layoutStore) get(key string) *userLayouts {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[key]
	if !ok {
		u = &userLayouts{}
		s.users[key] = u
	}
	return u
}

// anonymousKeyCookie names visitors who are not signed in, so their layouts
// can be kept for them. Signed-in users are keyed by their user ID instead,
// so their layouts follow them from browser to browser.
const anonymousKeyCookie = "layout-manager-key"

const anonymousKeyMaxAge = 365 * 24 * 60 * 60

// ownerKey identifies whose layouts the request reads and writes.
func ownerKey(w http.ResponseWriter, r *http.Request) string {
	if u := requestUser(w, r); u != nil {
		return "user:" + strconv.FormatUint(uint64(u.GetID()), 10)
	}
	if c, err := r.Cookie(anonymousKeyCookie); err == nil && validAnonymousKey(c.Value) {
		return "anon:" + c.Value
	}
	key := newAnonymousKey()
	http.SetCookie(w, sessions.NewCookie(anonymousKeyCookie, key, cookieOptions(r, anonymousKeyMaxAge)))
	// The rest of this request (a handler can ask twice) uses the same key.
	r.AddCookie(&http.Cookie{Name: anonymousKeyCookie, Value: key})
	return "anon:" + key
}

func requestUser(w http.ResponseWriter, r *http.Request) user.IUser {
	if u, ok := r.Context().Value(ctxkeys.User).(user.IUser); ok && u != nil {
		return u
	}
	// Without the auth middleware in front, Iridium reads the session.
	if auth.Store == nil || config.Config == nil {
		return nil
	}
	ctx := ctxBase.NewContext(r, w)
	if u, ok := ctx.User(); ok {
		return u
	}
	return nil
}

// cookieOptions are the ones Iridium gives its own cookies: Secure only over
// HTTPS, HttpOnly, and the configured SameSite and path.
func cookieOptions(r *http.Request, maxAge int) *sessions.Options {
	if config.Config == nil {
		return &sessions.Options{Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil}
	}
	return config.Config.CookieOptions(r, maxAge)
}

func newAnonymousKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("layoutmgr: no randomness for a visitor key: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func validAnonymousKey(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// clone deep-copies a layout state, so the store never shares slices with a
// request, and the plugin never shares them with the application's hooks.
func clone(s LayoutState) LayoutState {
	out := LayoutState{Layouts: make([]Layout, len(s.Layouts))}
	for i, l := range s.Layouts {
		out.Layouts[i] = Layout{Name: l.Name, Blocks: append([]Block(nil), l.Blocks...)}
	}
	return out
}
