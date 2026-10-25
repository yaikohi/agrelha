package compose

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func gmodPermSpec(uid int) func(domain.Instance) domain.RuntimeSpec {
	return func(domain.Instance) domain.RuntimeSpec {
		return domain.RuntimeSpec{
			Image:     "ceifa/garrysmod",
			RunAsUser: uid,
			Volumes:   []domain.VolumeSpec{{Name: "data", MountPath: "/home/gmod/server/garrysmod/data"}},
			Ports:     []domain.PortSpec{{Name: "game", Port: 27015, Protocol: "UDP"}},
		}
	}
}

func renderGMod(t *testing.T, uid int) string {
	t.Helper()
	inst := domain.Instance{GameID: domain.GameGMod, Number: 1, Name: "Lareira", Slug: "lareira", Host: "fogo"}
	files, err := New(domain.GModProfile, "/var/lib/agrelha/data", gmodPermSpec(uid)).Render(inst, domain.ModList{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(files[ComposeFileName])
}

func TestAnImageThatDropsPrivilegeGetsItsDataDirectoryChowned(t *testing.T) {
	out := renderGMod(t, 1000)

	if !strings.Contains(out, "data-perms") {
		t.Fatalf(`the rendered compose has no step that fixes data ownership.

agrelha creates the data directory as its own user (uid 999), but this image drops to uid 1000
before running the server, so it cannot read or write its own data/: no saves, no admin list, no
logs, and no error anywhere. Fixed by hand once already; without this it returns on every recreate.
%s`, out)
	}
	if !strings.Contains(out, "chown -R 1000 ") {
		t.Errorf("the ownership step does not chown to the uid the image runs as:\n%s", out)
	}
	if strings.Contains(out, "chown -R 1000:1000") {
		t.Error(`the step also takes the group, which locks agrelha out of the directory it created.

agrelha runs as uid 999 and must keep group access - it reads this tree for logs and backups - so
the fix sets the owner and leaves the group alone, adding group rwX instead.`)
	}
	if !strings.Contains(out, "chmod -R g+rwX") {
		t.Error("the group was left without access, so agrelha cannot read what the server writes")
	}
	if !strings.Contains(out, "service_completed_successfully") {
		t.Error("the server does not wait for the ownership fix, so it can start before the directory is usable")
	}
}

func TestAnImageRunningAsRootNeedsNoOwnershipFix(t *testing.T) {
	out := renderGMod(t, 0)
	if strings.Contains(out, "data-perms") {
		t.Error("a root image got a pointless chown step; Valheim runs as root and must stay unchanged")
	}
}

func TestTheOwnershipFixCoversEveryVolume(t *testing.T) {
	spec := func(domain.Instance) domain.RuntimeSpec {
		return domain.RuntimeSpec{
			Image:     "x",
			RunAsUser: 1000,
			Volumes: []domain.VolumeSpec{
				{Name: "data", MountPath: "/data"},
				{Name: "custom-configs", MountPath: "/custom-configs", ReadOnly: true},
			},
		}
	}
	inst := domain.Instance{GameID: domain.GameGMod, Number: 2, Name: "B", Slug: "b", Host: "fogo"}
	files, err := New(domain.GModProfile, "/var/lib/agrelha/data", spec).Render(inst, domain.ModList{})
	if err != nil {
		t.Fatal(err)
	}
	out := string(files[ComposeFileName])
	for _, want := range []string{"/mnt/data", "/mnt/custom-configs"} {
		if !strings.Contains(out, want) {
			t.Errorf("volume %q was left with the wrong owner:\n%s", want, out)
		}
	}
}
