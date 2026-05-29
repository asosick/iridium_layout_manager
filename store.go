package layoutmgr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/iridiumgo/iridium/bootstrap/auth"
	"github.com/iridiumgo/iridium/core/logger"
)

// LoadHook fetches the LayoutState for the current request. Return a zero
// LayoutState{} (not nil) when nothing is stored yet.
type LoadHook func(r *http.Request) (LayoutState, error)

// SaveHook persists the LayoutState for the current request.
type SaveHook func(r *http.Request, state LayoutState) error

var (
	errNoWriter       = errors.New("layoutmgr: no ResponseWriter on the request context — call attachWriter from your handler")
	errAuthStoreUnset = errors.New("layoutmgr: auth.Store is not configured; either configure iridium's auth Store or provide your own SaveHook/LoadHook")
)

// sessionLoad / sessionSave are the default persistence: per-user gorilla
// session under a key derived from the page slug. No DB required — works
// out of the box.
//
// State is JSON-encoded so it survives the session codec without needing
// every consuming app to gob-register the LayoutState type.
func sessionLoad(sessionKey string) LoadHook {
	return func(r *http.Request) (LayoutState, error) {
		var zero LayoutState
		sess, err := openSession(r)
		if err != nil {
			return zero, err
		}
		raw, ok := sess.Values[sessionKey]
		if !ok {
			return zero, nil
		}
		s, ok := raw.(string)
		if !ok {
			logger.Warn("[layoutmgr] unexpected session layout type %T for key %s", raw, sessionKey)
			return zero, nil
		}
		if s == "" {
			return zero, nil
		}
		var state LayoutState
		if err := json.Unmarshal([]byte(s), &state); err != nil {
			// Soft-fail — start fresh rather than 500 a user out of their
			// dashboard because of a stale codec.
			logger.Warn("[layoutmgr] failed to decode session layout (%s): %v", sessionKey, err)
			return zero, nil
		}
		return state, nil
	}
}

func sessionSave(sessionKey string) SaveHook {
	return func(r *http.Request, state LayoutState) error {
		sess, err := openSession(r)
		if err != nil {
			return err
		}
		buf, err := json.Marshal(state)
		if err != nil {
			return err
		}
		sess.Values[sessionKey] = string(buf)
		// gorilla.Save needs the ResponseWriter (to emit Set-Cookie). Our
		// route handlers stash it on the request context via attachWriter.
		w, ok := writerFromRequest(r)
		if !ok {
			return errNoWriter
		}
		return sess.Save(r, w)
	}
}

// sessionInitialized reports whether the working buffer for sessionKey has
// already been seeded this session. We track a separate boolean marker so we
// can distinguish "buffer holds an empty layout because the user deleted every
// block" from "buffer was never seeded from the durable store". Without it,
// an empty buffer would be re-seeded from loadHook on the next render and the
// user's deletions would reappear.
func sessionInitialized(r *http.Request, sessionKey string) (bool, error) {
	sess, err := openSession(r)
	if err != nil {
		return false, err
	}
	v, ok := sess.Values[sessionKey+":init"]
	if !ok {
		return false, nil
	}
	b, _ := v.(bool)
	return b, nil
}

// markSessionInitialized flips the init marker for sessionKey. Persisting it
// requires the ResponseWriter (stashed via attachWriter) just like sessionSave.
func markSessionInitialized(r *http.Request, sessionKey string) error {
	sess, err := openSession(r)
	if err != nil {
		return err
	}
	sess.Values[sessionKey+":init"] = true
	w, ok := writerFromRequest(r)
	if !ok {
		return errNoWriter
	}
	return sess.Save(r, w)
}

// openSession returns the gorilla session for layout state. Stored alongside
// auth in the same store but under a distinct session name so clearing one
// doesn't nuke the other.
func openSession(r *http.Request) (*sessions.Session, error) {
	if auth.Store == nil {
		return nil, errAuthStoreUnset
	}
	return auth.Store.Get(r, "layout-manager")
}

// --- request-scoped response writer plumbing ---------------------------------
//
// The SaveHook signature is `func(*http.Request, LayoutState) error` — no
// ResponseWriter. But gorilla session.Save needs the writer. We stash w on
// the request context from each handler so the default session save can find
// it without changing the public hook signature (custom DB-backed hooks
// don't need it).

type writerCtxKey struct{}

func attachWriter(r *http.Request, w http.ResponseWriter) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), writerCtxKey{}, w))
}

func writerFromRequest(r *http.Request) (http.ResponseWriter, bool) {
	v := r.Context().Value(writerCtxKey{})
	if v == nil {
		return nil, false
	}
	w, ok := v.(http.ResponseWriter)
	return w, ok
}
