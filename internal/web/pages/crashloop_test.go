package pages

import "slices"

import "testing"

// The blocker was not that Delete was missing but that it was gated on a state
// the runtime reported wrongly: a crash-looping world said "running", so the
// only button that could clear it never appeared.
func TestACrashLoopingInstanceCanBeDeleted(t *testing.T) {
	inst := InstanceUI{Number: 1, Name: "vp", State: "error", IsAdmin: true}

	var labels []string
	for _, a := range managerActions(minecraftSpec, inst) {
		labels = append(labels, a.Label)
	}

	if !contains(labels, "Delete") {
		t.Errorf("a crash-looping world must be deletable, got actions %v", labels)
	}
	if got := managerStateLabel(inst); got != "Crash loop" {
		t.Errorf("state label = %q, want %q", got, "Crash loop")
	}
	if got := managerLamp(inst); got != "lamp lamp-out" {
		t.Errorf("lamp = %q; a world that will not stay up is not lit", got)
	}
}

func TestARunningInstanceStillCannotBeDeleted(t *testing.T) {
	inst := InstanceUI{Number: 1, Name: "vp", State: "running", IsAdmin: true}

	for _, a := range managerActions(minecraftSpec, inst) {
		if a.Label == "Delete" {
			t.Error("a running world must be stopped before it can be deleted")
		}
	}
}

func contains(ss []string, want string) bool {
	return slices.Contains(ss, want)
}
