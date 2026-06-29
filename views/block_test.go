package views

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestBlockRendersSortKey(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{AllowReorder: true}
	b := BlockData{ID: "block-id", Cols: 1, Renderer: templ.Raw("content")}

	if err := Block(d, b).Render(context.Background(), &out); err != nil {
		t.Fatalf("render block: %v", err)
	}
	if html := out.String(); !strings.Contains(html, "x-sort:item=") || !strings.Contains(html, "block-id") {
		t.Fatalf("expected block UUID as Alpine sort key, got %s", html)
	}
}

func TestBlockRendersMoveAndResizeControls(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{AllowReorder: true, AllowResize: true}
	b := BlockData{
		ID:           "block-id",
		Cols:         1,
		Renderer:     templ.Raw("content"),
		ResizeURL:    "/resize?id=block-id",
		MoveLeftURL:  "/move?id=block-id&delta=-1",
		MoveRightURL: "/move?id=block-id&delta=1",
		CanMoveRight: true,
	}

	if err := Block(d, b).Render(context.Background(), &out); err != nil {
		t.Fatalf("render block: %v", err)
	}
	html := out.String()
	for _, expected := range []string{"data-lm-resize-handle", "Move block left", "Move block right"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("expected %q in block controls, got %s", expected, html)
		}
	}
}
