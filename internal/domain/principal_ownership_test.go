package domain

import "testing"

func TestCanOperate_CreatorNeedsNoRole(t *testing.T) {
	p := NewPrincipal(Identity{Subject: "u1", Roles: []Role{RoleUser}}).
		WithOwned([]InstanceID{NewInstanceID(GameMinecraft, 1)})

	if !p.CanOperate(GameMinecraft, 1) {
		t.Error("the creator of a world must be able to operate it immediately, without waiting for the Zitadel role to reach their session")
	}
	if p.CanOperate(GameMinecraft, 2) {
		t.Error("creating world #1 must not grant access to #2")
	}
	if p.CanOperate(GameValheim, 1) {
		t.Error("creating minecraft #1 must not grant access to valheim #1")
	}
}

func TestCanOperate_RoleStillWorksForNonCreators(t *testing.T) {
	p := NewPrincipal(Identity{Subject: "u2", Roles: []Role{RoleUser, InstanceRole(GameValheim, 3)}})

	if !p.CanOperate(GameValheim, 3) {
		t.Error("a granted per-instance role must still grant access when the user did not create the world")
	}
	if p.CanOperate(GameValheim, 4) {
		t.Error("a role for #3 must not grant access to #4")
	}
}

func TestCanOperate_SignedOutOwnsNothing(t *testing.T) {
	var p Principal
	p = p.WithOwned([]InstanceID{NewInstanceID(GameMinecraft, 1)})
	if p.CanOperate(GameMinecraft, 1) {
		t.Error("an unauthenticated principal must not operate anything, ownership data notwithstanding")
	}
}
