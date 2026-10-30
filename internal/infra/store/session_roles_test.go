package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func seedSession(t *testing.T, s *Store, id, subject string, roles ...domain.Role) {
	t.Helper()
	err := s.CreateSession(context.Background(), ports.Session{
		ID: id, Subject: subject, Email: subject + "@example.com",
		Roles: roles, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("seed session %s: %v", id, err)
	}
}

func sessionRoles(t *testing.T, s *Store, id string) []domain.Role {
	t.Helper()
	sess, err := s.LoadSession(context.Background(), id)
	if err != nil || sess == nil {
		t.Fatalf("load session %s: %v", id, err)
	}
	return sess.Roles
}

func TestAddSessionRole_GrantReachesTheLiveSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "sid-a", "user-a", domain.RoleUser)
	seedSession(t, s, "sid-b", "user-b", domain.RoleUser)

	role := domain.InstanceRole(domain.GameMinecraft, 1)
	if err := s.AddSessionRole(ctx, "user-a", role); err != nil {
		t.Fatalf("AddSessionRole: %v", err)
	}

	if !slices.Contains(sessionRoles(t, s, "sid-a"), role) {
		t.Error("a granted role must reach the subject's live session, otherwise access waits for re-login")
	}
	if slices.Contains(sessionRoles(t, s, "sid-b"), role) {
		t.Error("granting to user-a must not touch user-b's session")
	}

	if err := s.AddSessionRole(ctx, "user-a", role); err != nil {
		t.Fatalf("AddSessionRole twice: %v", err)
	}
	if got := sessionRoles(t, s, "sid-a"); len(got) != 2 {
		t.Errorf("granting the same role twice must not duplicate it, got %v", got)
	}
}

func TestRemoveSessionRole_RevokeTakesEffectImmediately(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	role := domain.InstanceRole(domain.GameValheim, 2)
	seedSession(t, s, "sid-a", "user-a", domain.RoleUser, role)

	if err := s.RemoveSessionRole(ctx, "user-a", role); err != nil {
		t.Fatalf("RemoveSessionRole: %v", err)
	}
	if slices.Contains(sessionRoles(t, s, "sid-a"), role) {
		t.Error("a revoked role must leave the live session; otherwise the user keeps operating the world until it expires")
	}
}

func TestRemoveSessionRoleEverywhere_SlotReuseDoesNotInheritAccess(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	role := domain.InstanceRole(domain.GameMinecraft, 1)
	seedSession(t, s, "sid-a", "user-a", domain.RoleUser, role)
	seedSession(t, s, "sid-b", "user-b", domain.RoleUser, role)

	if err := s.RemoveSessionRoleEverywhere(ctx, role); err != nil {
		t.Fatalf("RemoveSessionRoleEverywhere: %v", err)
	}
	for _, id := range []string{"sid-a", "sid-b"} {
		if slices.Contains(sessionRoles(t, s, id), role) {
			t.Errorf("%s kept a role naming a recycled slot; the next world in that slot would be reachable for free", id)
		}
		if !slices.Contains(sessionRoles(t, s, id), domain.RoleUser) {
			t.Errorf("%s lost unrelated roles", id)
		}
	}
}
