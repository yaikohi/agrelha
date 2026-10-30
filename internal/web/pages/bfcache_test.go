package pages

import (
	"context"
	"strings"
	"testing"
)

func TestLayoutReloadsOnRestoredPage(t *testing.T) {
	var sb strings.Builder
	if err := LayoutHearth("x").Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := sb.String()

	if !strings.Contains(got, "pageshow") || !strings.Contains(got, "e.persisted") {
		t.Error("every page must refetch when the browser restores it from the back-forward cache; Firefox restores Cache-Control: no-store pages, so a signed-out user would keep seeing the signed-in page")
	}
	if !strings.Contains(got, "window.location.reload()") {
		t.Error("the pageshow handler does not actually reload")
	}
}
