package layoutmgr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersistAndRenderUsesSessionStoreOnly(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	sessionSaves := 0
	durableSaves := 0
	p.sessionSave = func(_ *http.Request, _ LayoutState) error {
		sessionSaves++
		return nil
	}
	p.saveHook = func(_ *http.Request, _ LayoutState) error {
		durableSaves++
		return nil
	}

	r := httptest.NewRequest(http.MethodPost, "/lm/reorder", nil)
	w := httptest.NewRecorder()
	p.persistAndRender(w, r, LayoutState{}, 0)

	if sessionSaves != 1 {
		t.Fatalf("expected one session save, got %d", sessionSaves)
	}
	if durableSaves != 0 {
		t.Fatalf("durable hook ran before explicit save: %d", durableSaves)
	}
}

func TestPersistAndRenderReturnsErrorWhenSessionSaveFails(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	p.sessionSave = func(_ *http.Request, _ LayoutState) error {
		return errors.New("session unavailable")
	}

	r := httptest.NewRequest(http.MethodPost, "/lm/reorder", nil)
	w := httptest.NewRecorder()
	p.persistAndRender(w, r, LayoutState{}, 0)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}

func TestHandleSaveCommitsSessionState(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	want := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "a"}}}}}
	p.sessionLoad = func(_ *http.Request) (LayoutState, error) { return want, nil }
	p.sessionExists = func(_ *http.Request) (bool, error) { return true, nil }
	sessionSaves := 0
	committedSaves := 0
	durableSaves := 0
	p.sessionSave = func(_ *http.Request, _ LayoutState) error {
		sessionSaves++
		return nil
	}
	p.saveHook = func(_ *http.Request, got LayoutState) error {
		durableSaves++
		if got.Layouts[0].Blocks[0].ID != "a" {
			t.Fatalf("save hook received wrong state: %+v", got)
		}
		return nil
	}
	p.committedSave = func(_ *http.Request, got LayoutState) error {
		committedSaves++
		if got.Layouts[0].Blocks[0].ID != "a" {
			t.Fatalf("committed snapshot received wrong state: %+v", got)
		}
		return nil
	}

	r := httptest.NewRequest(http.MethodPost, "/lm/save", nil)
	w := httptest.NewRecorder()
	p.handleSave(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", w.Code)
	}
	if sessionSaves != 1 || committedSaves != 1 || durableSaves != 1 {
		t.Fatalf("expected session, committed and durable saves once, got %d, %d and %d", sessionSaves, committedSaves, durableSaves)
	}
}

func TestHandleResetRestoresCommittedState(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	draft := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "draft"}}}}}
	committed := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "saved"}}}}}
	p.sessionLoad = func(_ *http.Request) (LayoutState, error) { return draft, nil }
	p.sessionExists = func(_ *http.Request) (bool, error) { return true, nil }
	p.committedExists = func(_ *http.Request) (bool, error) { return true, nil }
	p.committedLoad = func(_ *http.Request) (LayoutState, error) { return committed, nil }
	var restored LayoutState
	p.sessionSave = func(_ *http.Request, state LayoutState) error {
		restored = state
		return nil
	}

	r := httptest.NewRequest(http.MethodPost, "/lm/reset?layout=0", nil)
	w := httptest.NewRecorder()
	p.handleReset(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if restored.Layouts[0].Blocks[0].ID != "saved" {
		t.Fatalf("expected committed state, got %+v", restored)
	}
}

func TestPrepareMutationCapturesInitialBaseline(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	p.committedExists = func(_ *http.Request) (bool, error) { return false, nil }
	var captured LayoutState
	p.committedSave = func(_ *http.Request, state LayoutState) error {
		captured = state
		return nil
	}
	state := LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "baseline"}}}}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/lm/move", nil)
	if !p.prepareMutation(w, r, state) {
		t.Fatalf("expected baseline capture to succeed: %s", w.Body.String())
	}
	if captured.Layouts[0].Blocks[0].ID != "baseline" {
		t.Fatalf("expected baseline state, got %+v", captured)
	}
}

func TestReorderResponseDoesNotAppendSelectors(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	p.sessionSave = func(_ *http.Request, _ LayoutState) error { return nil }
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/lm/reorder", nil)

	p.persistAndRenderGridOnly(w, r, LayoutState{}, 0)

	if body := w.Body.String(); strings.Contains(body, "hx-swap-oob") || strings.Contains(body, "lm-selectors") {
		t.Fatalf("grid-only response included selector markup: %s", body)
	}
}

func TestCurrentStatePrefersSessionDraft(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	p.sessionLoad = func(_ *http.Request) (LayoutState, error) {
		return LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "draft"}}}}}, nil
	}
	p.sessionExists = func(_ *http.Request) (bool, error) { return true, nil }
	loadCalls := 0
	p.loadHook = func(_ *http.Request) (LayoutState, error) {
		loadCalls++
		return LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "durable"}}}}}, nil
	}

	state := p.currentState(httptest.NewRequest(http.MethodGet, "/", nil))
	if state.Layouts[0].Blocks[0].ID != "draft" {
		t.Fatalf("expected session draft, got %+v", state)
	}
	if loadCalls != 0 {
		t.Fatalf("durable load hook ran despite an existing draft")
	}
}

func TestCurrentStateKeepsEmptySessionDraft(t *testing.T) {
	p := NewLayoutManagerPage("Dashboard", "dashboard")
	p.sessionLoad = func(_ *http.Request) (LayoutState, error) { return LayoutState{}, nil }
	p.sessionExists = func(_ *http.Request) (bool, error) { return true, nil }
	p.loadHook = func(_ *http.Request) (LayoutState, error) {
		return LayoutState{Layouts: []Layout{{Blocks: []Block{{ID: "durable"}}}}}, nil
	}

	state := p.currentState(httptest.NewRequest(http.MethodGet, "/", nil))
	if len(state.Layouts[0].Blocks) != 0 {
		t.Fatalf("expected empty session draft, got %+v", state)
	}
}
