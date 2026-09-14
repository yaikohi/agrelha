package access

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agrelha/internal/ports"
)

type mockStateStore struct {
	docs map[string]ports.Document
}

func (s *mockStateStore) Get(_ context.Context, path string) (ports.Document, error) {
	if s.docs == nil {
		return ports.Document{}, nil
	}
	return s.docs[path], nil
}
func (s *mockStateStore) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	s.docs[path] = doc
	return nil
}
func (s *mockStateStore) Delete(_ context.Context, path, _ string) error {
	delete(s.docs, path)
	return nil
}
func (s *mockStateStore) PutTree(_ context.Context, _ string, _ map[string]ports.Document, _ string) error {
	return nil
}
func (s *mockStateStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	doc := s.docs[path]
	changed, err := fn(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		s.docs[path] = doc
	}
	return changed, nil
}

type mockConsole struct {
	commands []string
	response string
	err      error
}

func (c *mockConsole) Execute(cmd string) (string, error) {
	c.commands = append(c.commands, cmd)
	if c.err != nil {
		return "", c.err
	}
	return c.response, nil
}

type mockAuditRecorder struct {
	entries []string
}

func (a *mockAuditRecorder) RecordAudit(actor, action, detail string) error {
	a.entries = append(a.entries, fmt.Sprintf("%s:%s:%s", actor, action, detail))
	return nil
}

func TestAccessManagerListAccess(t *testing.T) {
	ctx := context.Background()

	// 1. With accessReader option
	mgrWithReader := NewAccessManager(nil, "", nil, WithAccessReader(func(ctx context.Context) ([]string, []string, error) {
		return []string{"admin1"}, []string{"player1", "player2"}, nil
	}))
	ops, wl, err := mgrWithReader.ListAccess(ctx)
	if err != nil || len(ops) != 1 || len(wl) != 2 {
		t.Fatalf("unexpected ListAccess with reader: ops=%v, wl=%v, err=%v", ops, wl, err)
	}

	// 2. With state store
	ss := &mockStateStore{
		docs: map[string]ports.Document{
			"manifests/neoforge-access.yaml": {
				Data: map[string]string{
					"ops.txt":       "# Op list\nsteve\nalex\n",
					"whitelist.txt": "# Whitelist\nalex\nnotch\n",
				},
			},
		},
	}
	mgrStore := NewAccessManager(ss, "manifests/neoforge-access.yaml", nil)
	ops, wl, err = mgrStore.ListAccess(ctx)
	if err != nil {
		t.Fatalf("unexpected error from ListAccess: %v", err)
	}
	if len(ops) != 2 || ops[0] != "steve" || ops[1] != "alex" {
		t.Errorf("unexpected ops: %v", ops)
	}
	if len(wl) != 2 || wl[0] != "alex" || wl[1] != "notch" {
		t.Errorf("unexpected whitelist: %v", wl)
	}
}

func TestAccessManagerGrantAndRevokeOp(t *testing.T) {
	ctx := context.Background()
	ss := &mockStateStore{
		docs: map[string]ports.Document{
			"manifests/neoforge-access.yaml": {
				Data: map[string]string{
					"ops.txt": "steve\n",
				},
			},
		},
	}
	console := &mockConsole{}
	audit := &mockAuditRecorder{}

	mgr := NewAccessManager(ss, "manifests/neoforge-access.yaml", console, WithAudit(audit))

	// 1. GrantOp for a new user
	changed, err := mgr.GrantOp(ctx, "alex", "admin")
	if err != nil || !changed {
		t.Fatalf("GrantOp failed: changed=%v, err=%v", changed, err)
	}
	if len(console.commands) != 1 || console.commands[0] != "/op alex" {
		t.Errorf("expected RCON /op alex, got: %v", console.commands)
	}
	if len(audit.entries) != 1 || !strings.Contains(audit.entries[0], "mc-op-grant") {
		t.Errorf("expected audit entry for grant, got: %v", audit.entries)
	}

	// 2. GrantOp for existing user is a no-op
	changed, err = mgr.GrantOp(ctx, "alex", "admin")
	if err != nil || changed {
		t.Fatalf("expected no-op for existing op: changed=%v, err=%v", changed, err)
	}

	// 3. RevokeOp
	console.commands = nil
	audit.entries = nil
	changed, err = mgr.RevokeOp(ctx, "steve", "admin")
	if err != nil || !changed {
		t.Fatalf("RevokeOp failed: changed=%v, err=%v", changed, err)
	}
	if len(console.commands) != 1 || console.commands[0] != "/deop steve" {
		t.Errorf("expected RCON /deop steve, got: %v", console.commands)
	}
	if len(audit.entries) != 1 || !strings.Contains(audit.entries[0], "mc-op-revoke") {
		t.Errorf("expected audit entry for revoke, got: %v", audit.entries)
	}

	// 4. RevokeOp for non-existent user
	changed, err = mgr.RevokeOp(ctx, "nonexistent", "admin")
	if err != nil || changed {
		t.Fatalf("expected no-op for nonexistent op: changed=%v, err=%v", changed, err)
	}
}

func TestAccessManagerAddAndRemoveWhitelist(t *testing.T) {
	ctx := context.Background()
	ss := &mockStateStore{
		docs: map[string]ports.Document{
			"manifests/neoforge-access.yaml": {
				Data: map[string]string{
					"whitelist.txt": "player1\n",
				},
			},
		},
	}
	console := &mockConsole{}
	audit := &mockAuditRecorder{}

	mgr := NewAccessManager(ss, "manifests/neoforge-access.yaml", console, WithAudit(audit))

	// 1. AddWhitelist for new user
	changed, err := mgr.AddWhitelist(ctx, "player2", "moderator")
	if err != nil || !changed {
		t.Fatalf("AddWhitelist failed: changed=%v, err=%v", changed, err)
	}
	expectedCmds := []string{"/whitelist add player2", "/whitelist reload"}
	for i, exp := range expectedCmds {
		if i >= len(console.commands) || console.commands[i] != exp {
			t.Errorf("expected command %s, got: %v", exp, console.commands)
		}
	}
	if len(audit.entries) != 1 || !strings.Contains(audit.entries[0], "mc-whitelist-add") {
		t.Errorf("expected audit entry for whitelist add, got: %v", audit.entries)
	}

	// 2. AddWhitelist for existing user is a no-op
	changed, err = mgr.AddWhitelist(ctx, "player2", "moderator")
	if err != nil || changed {
		t.Fatalf("expected no-op for existing whitelist player: changed=%v, err=%v", changed, err)
	}

	// 3. RemoveWhitelist
	console.commands = nil
	audit.entries = nil
	changed, err = mgr.RemoveWhitelist(ctx, "player1", "moderator")
	if err != nil || !changed {
		t.Fatalf("RemoveWhitelist failed: changed=%v, err=%v", changed, err)
	}
	expectedRemoveCmds := []string{"/whitelist remove player1", "/whitelist reload"}
	for i, exp := range expectedRemoveCmds {
		if i >= len(console.commands) || console.commands[i] != exp {
			t.Errorf("expected command %s, got: %v", exp, console.commands)
		}
	}

	// 4. RemoveWhitelist for non-existent user
	changed, err = mgr.RemoveWhitelist(ctx, "nonexistent", "moderator")
	if err != nil || changed {
		t.Fatalf("expected no-op for non-existent whitelist player: changed=%v, err=%v", changed, err)
	}
}

func TestAccessManagerOnlinePlayersAndParseList(t *testing.T) {
	// ParsePlayerList
	raw := "There are 3 of a max of 20 players online: notch, jeb_, dinnerbone"
	players := ParsePlayerList(raw)
	if len(players) != 3 || players[0] != "notch" || players[1] != "jeb_" || players[2] != "dinnerbone" {
		t.Errorf("unexpected players: %v", players)
	}

	// Empty list
	if players := ParsePlayerList("There are 0 of a max of 20 players online: "); len(players) != 0 {
		t.Errorf("expected 0 players, got: %v", players)
	}
	if players := ParsePlayerList("invalid string"); len(players) != 0 {
		t.Errorf("expected 0 players for invalid, got: %v", players)
	}

	// OnlinePlayers via RCON
	console := &mockConsole{response: raw}
	mgr := NewAccessManager(nil, "", console)
	online, err := mgr.OnlinePlayers()
	if err != nil || len(online) != 3 {
		t.Fatalf("OnlinePlayers failed: online=%v, err=%v", online, err)
	}
}

func TestAccessManagerWhitelistEnforced(t *testing.T) {
	// 1. Probe when already on
	consoleOn := &mockConsole{response: "Whitelist is already turned on"}
	mgrOn := NewAccessManager(nil, "", consoleOn)
	enforced, err := mgrOn.WhitelistEnforced()
	if err != nil || !enforced {
		t.Errorf("expected enforced=true, got: %v, err=%v", enforced, err)
	}

	// 2. Probe when previously off
	consoleOff := &mockConsole{response: "Whitelist is now turned on"}
	mgrOff := NewAccessManager(nil, "", consoleOff)
	enforced, err = mgrOff.WhitelistEnforced()
	if err != nil || enforced {
		t.Errorf("expected enforced=false, got: %v, err=%v", enforced, err)
	}
	// Verify it reverted with "/whitelist off"
	if len(consoleOff.commands) < 2 || consoleOff.commands[1] != "/whitelist off" {
		t.Errorf("expected revert /whitelist off, got: %v", consoleOff.commands)
	}

	// 3. SetWhitelistEnforced on and off
	audit := &mockAuditRecorder{}
	consoleToggle := &mockConsole{}
	mgrToggle := NewAccessManager(nil, "", consoleToggle, WithAudit(audit))

	if err := mgrToggle.SetWhitelistEnforced(true, "admin"); err != nil {
		t.Fatalf("SetWhitelistEnforced(true) failed: %v", err)
	}
	if len(consoleToggle.commands) != 1 || consoleToggle.commands[0] != "/whitelist on" {
		t.Errorf("expected /whitelist on, got: %v", consoleToggle.commands)
	}

	if err := mgrToggle.SetWhitelistEnforced(false, "admin"); err != nil {
		t.Fatalf("SetWhitelistEnforced(false) failed: %v", err)
	}
	if len(consoleToggle.commands) != 2 || consoleToggle.commands[1] != "/whitelist off" {
		t.Errorf("expected /whitelist off, got: %v", consoleToggle.commands)
	}
}

type failingAccessStore struct {
	getErr   error
	patchErr error
}

func (f *failingAccessStore) Get(context.Context, string) (ports.Document, error) {
	return ports.Document{}, f.getErr
}
func (f *failingAccessStore) Put(context.Context, string, ports.Document, string) error { return nil }
func (f *failingAccessStore) Delete(context.Context, string, string) error              { return nil }
func (f *failingAccessStore) PutTree(context.Context, string, map[string]ports.Document, string) error {
	return nil
}
func (f *failingAccessStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if f.patchErr != nil {
		return false, f.patchErr
	}
	var doc ports.Document
	return fn(&doc)
}

func TestAccessManagerNilStoreAndErrors(t *testing.T) {
	ctx := context.Background()

	// 1. Nil store returns ErrNotImplemented
	nilMgr := NewAccessManager(nil, "", nil)
	if _, _, err := nilMgr.ListAccess(ctx); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for ListAccess, got %v", err)
	}
	if _, err := nilMgr.GrantOp(ctx, "steve"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for GrantOp, got %v", err)
	}
	if _, err := nilMgr.RevokeOp(ctx, "steve"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for RevokeOp, got %v", err)
	}
	if _, err := nilMgr.AddWhitelist(ctx, "steve"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for AddWhitelist, got %v", err)
	}
	if _, err := nilMgr.RemoveWhitelist(ctx, "steve"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for RemoveWhitelist, got %v", err)
	}
	if _, err := nilMgr.WhitelistEnforced(); err == nil {
		t.Errorf("expected error for WhitelistEnforced without rcon")
	}
	if err := nilMgr.SetWhitelistEnforced(true); err == nil {
		t.Errorf("expected error for SetWhitelistEnforced without rcon")
	}

	// 2. Get error
	getFailMgr := NewAccessManager(&failingAccessStore{getErr: context.Canceled}, "path", nil)
	if _, _, err := getFailMgr.ListAccess(ctx); err != context.Canceled {
		t.Errorf("expected Get error, got %v", err)
	}

	// 3. Patch error
	patchFailMgr := NewAccessManager(&failingAccessStore{patchErr: context.Canceled}, "path", nil)
	if _, err := patchFailMgr.GrantOp(ctx, "steve"); err != context.Canceled {
		t.Errorf("expected GrantOp patch error, got %v", err)
	}
	if _, err := patchFailMgr.RevokeOp(ctx, "steve"); err != context.Canceled {
		t.Errorf("expected RevokeOp patch error, got %v", err)
	}
	if _, err := patchFailMgr.AddWhitelist(ctx, "steve"); err != context.Canceled {
		t.Errorf("expected AddWhitelist patch error, got %v", err)
	}
	if _, err := patchFailMgr.RemoveWhitelist(ctx, "steve"); err != context.Canceled {
		t.Errorf("expected RemoveWhitelist patch error, got %v", err)
	}

	// 4. nil doc.Data in GrantOp and AddWhitelist
	nilDocStore := &failingAccessStore{}
	nilDocMgr := NewAccessManager(nilDocStore, "path", nil)
	if _, err := nilDocMgr.GrantOp(ctx, "alex"); err != nil {
		t.Errorf("GrantOp on nil doc failed: %v", err)
	}
	if _, err := nilDocMgr.AddWhitelist(ctx, "alex"); err != nil {
		t.Errorf("AddWhitelist on nil doc failed: %v", err)
	}

	// 5. WhitelistEnforced via rcon error
	consoleErr := &mockConsole{err: context.Canceled}
	rconErrMgr := NewAccessManager(nil, "", consoleErr)
	if _, err := rconErrMgr.WhitelistEnforced(); err == nil {
		t.Errorf("expected error from WhitelistEnforced on console error")
	}
}

func TestModManagerNilStoreAndErrors(t *testing.T) {
	ctx := context.Background()

	// 1. Nil store
	nilMods := NewModManager(nil, "")
	if _, err := nilMods.Install(ctx, []string{"jei"}); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented on Install, got %v", err)
	}
	if _, err := nilMods.Uninstall(ctx, "jei"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented on Uninstall, got %v", err)
	}
	if _, err := nilMods.SetVersion(ctx, "1.21.1", "latest"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented on SetVersion, got %v", err)
	}
	if _, err := nilMods.SwitchModpack(ctx, "pack", "1.21.1", []string{"mod1"}); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented on SwitchModpack, got %v", err)
	}

	// 2. Install on empty doc.Data
	nilDocStore := &failingAccessStore{}
	modMgr := NewModManager(nilDocStore, "path")
	if _, err := modMgr.Install(ctx, []string{"jei"}); err != nil {
		t.Errorf("Install on nil doc failed: %v", err)
	}
	if _, err := modMgr.SetVersion(ctx, "1.21.1", "recommended"); err != nil {
		t.Errorf("SetVersion failed: %v", err)
	}
	if _, err := modMgr.SetVersion(ctx, "1.21.1", ""); err != nil {
		t.Errorf("SetVersion with empty loaderVersion failed: %v", err)
	}
	// Empty loaderKey default
	modMgrEmptyKey := &ModManager{store: nilDocStore, path: "path"}
	if _, err := modMgrEmptyKey.SetVersion(ctx, "1.21.1", "latest"); err != nil {
		t.Errorf("SetVersion with empty loaderKey failed: %v", err)
	}
	if _, err := modMgr.SwitchModpack(ctx, "pack", "1.21.1", []string{"mod1"}); err != nil {
		t.Errorf("SwitchModpack failed: %v", err)
	}

	// 3. SetWhitelistEnforced rcon error & actorOrHyphen fallback
	consoleErr := &mockConsole{err: context.Canceled}
	auditRec := &mockAuditRecorder{}
	errMgr := NewAccessManager(nilDocStore, "path", consoleErr, WithAudit(auditRec))
	if err := errMgr.SetWhitelistEnforced(true, ""); err == nil {
		t.Errorf("expected error in SetWhitelistEnforced when rcon fails")
	}
	// GrantOp with empty actor
	_, _ = errMgr.GrantOp(ctx, "playerWithoutActor")
	if len(auditRec.entries) == 0 || auditRec.entries[len(auditRec.entries)-1] != "-:mc-op-grant:playerWithoutActor" {
		t.Errorf("expected '-' actor, got: %v", auditRec.entries)
	}
}


