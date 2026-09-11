package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLayoutManagerRendersNativeHeaderControls(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{LayoutCount: 3, ShowLockButton: true, ZenEnabled: true}

	if err := Toolbar(d).Render(context.Background(), &out); err != nil {
		t.Fatalf("render layout toolbar: %v", err)
	}
	html := out.String()
	if strings.Contains(html, "x-teleport") {
		t.Fatalf("native page header controls must not use teleport, got %s", html)
	}
	if !strings.Contains(html, `@htmx:after:request="finishEditing($event)"`) {
		t.Fatalf("expected Done to handle its completed request, got %s", html)
	}
	if !strings.Contains(html, `name="layout" x-model="currentLayout"`) {
		t.Fatalf("expected Done to submit the active layout, got %s", html)
	}
	for _, label := range []string{"Edit", "Zen"} {
		if !strings.Contains(html, label) {
			t.Fatalf("expected %q control, got %s", label, html)
		}
	}

	attrs := PageAttributes(d)
	if attrs["x-data"] != "layout_manager" || attrs["data-lm-page"] != "true" {
		t.Fatalf("expected native page controller attributes, got %#v", attrs)
	}
	if HeaderAttributes(d)["x-show"] != "!zenMode" {
		t.Fatalf("expected declarative Zen header visibility, got %#v", HeaderAttributes(d))
	}
}

func TestLayoutManagerOmitsZenControlsByDefault(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{ShowLockButton: true}

	if err := LayoutManagerContent(d).Render(context.Background(), &out); err != nil {
		t.Fatalf("render layout content: %v", err)
	}
	html := out.String()
	if strings.Contains(html, "Exit Zen") || strings.Contains(html, ">Zen<") {
		t.Fatalf("zen controls should be opt-in, got %s", html)
	}
}

func TestLayoutSelectorsExposeStableIndexes(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{LayoutCount: 3, LayoutContent: []bool{true, false, true}}

	if err := Toolbar(d).Render(context.Background(), &out); err != nil {
		t.Fatalf("render layout toolbar: %v", err)
	}
	html := out.String()
	for _, expected := range []string{
		`data-lm-layout="0"`,
		`data-lm-layout="1"`,
		`data-lm-layout="2"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("expected selector index %q, got %s", expected, html)
		}
	}
}
