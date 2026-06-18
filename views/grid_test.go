package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGridUsesNativeSortEventBridge(t *testing.T) {
	var out bytes.Buffer
	d := &PageData{AllowReorder: true, GridColumns: 2}

	if err := Grid(d).Render(context.Background(), &out); err != nil {
		t.Fatalf("render grid: %v", err)
	}
	html := out.String()
	if !strings.Contains(html, "x-sort") {
		t.Fatalf("expected Alpine sort directive, got %s", html)
	}
	if strings.Contains(html, `x-sort="handleReorder"`) {
		t.Fatalf("grid should not depend on Alpine's expression callback")
	}
}
