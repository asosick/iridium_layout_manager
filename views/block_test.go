package views

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/iridiumgo/iridium/config"
	"github.com/iridiumgo/iridium/core/context/ctxkeys"
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

// Under Iridium's CSP mode htmx ignores an element without the page's
// nonce: every control carries it.
func TestBlockControlsCarryTheCSPNonce(t *testing.T) {
	previous := config.Config
	t.Cleanup(func() { config.Config = previous })
	config.Config = config.NewIridiumConfig()
	config.Config.CSPMode = true
	ctx := context.WithValue(context.Background(), ctxkeys.NonceKey, "n0nce")
	var out bytes.Buffer
	d := &PageData{AllowReorder: true, AllowResize: true}
	b := BlockData{ID: "block-id", Cols: 1, Renderer: templ.Raw("content"), RemoveURL: "/remove"}
	if err := Block(d, b).Render(ctx, &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if posts, nonces := strings.Count(html, "hx-post="), strings.Count(html, `hx-nonce="n0nce"`); posts == 0 || posts != nonces {
		t.Fatalf("%d htmx controls, %d with the nonce", posts, nonces)
	}
}
