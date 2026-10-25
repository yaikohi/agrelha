package gmod

import (
	"testing"

	"agrelha/internal/domain"
)

func TestTheSpecDeclaresTheUidTheImageRunsAs(t *testing.T) {
	spec := New().RuntimeSpec(domain.Instance{
		GameID: domain.GameGMod, Number: 1, Name: "Lareira", Slug: "lareira",
		GMod: &domain.GModConfig{Gamemode: "sandbox", Map: "gm_construct"},
	})

	if spec.RunAsUser != 1000 {
		t.Errorf(`RuntimeSpec declares RunAsUser %d, but srcds runs as the "steam" user, uid 1000.

A Compose host creates the data directory as agrelha (uid 999). If the spec does not say which uid
the image drops to, the renderer emits no ownership fix and the server silently cannot read or write
its own data/: no ULX users, no saves, no logs, and no error anywhere. That cost hours once already.`, spec.RunAsUser)
	}
}
