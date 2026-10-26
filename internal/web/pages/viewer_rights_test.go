package pages

import "testing"

func TestTheViewerRightsFieldExists(t *testing.T) {
	var ui InstanceUI
	ui.CanOperate = true
	ui.IsAdmin = true
	if !ui.CanOperate || !ui.IsAdmin {
		t.Error("InstanceUI must carry both the viewer's admin flag and their per-world right")
	}
}
