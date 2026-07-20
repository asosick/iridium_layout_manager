package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLayoutManagerControlsRenderInPageHeader(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{LayoutCount: 3, ShowLockButton: true, ZenEnabled: true}

	if err := LayoutManagerComponent(d).Render(context.Background(), &out); err != nil {
		t.Fatalf("render layout manager: %v", err)
	}
	html := out.String()
	if !strings.Contains(html, `x-teleport=".ir-panel-page-actions"`) {
		t.Fatalf("expected controls to target the page header actions, got %s", html)
	}
	if !strings.Contains(html, `@htmx:after-request="finishEditing($event)"`) {
		t.Fatalf("expected Done to handle its request after being teleported, got %s", html)
	}
	for _, label := range []string{"Edit", "Zen", "Exit Zen"} {
		if !strings.Contains(html, label) {
			t.Fatalf("expected %q control, got %s", label, html)
		}
	}
}

func TestLayoutManagerOmitsZenControlsByDefault(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{ShowLockButton: true}

	if err := LayoutManagerComponent(d).Render(context.Background(), &out); err != nil {
		t.Fatalf("render layout manager: %v", err)
	}
	html := out.String()
	if strings.Contains(html, "Exit Zen") || strings.Contains(html, ">Zen<") {
		t.Fatalf("zen controls should be opt-in, got %s", html)
	}
}
