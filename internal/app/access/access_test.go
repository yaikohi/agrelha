package access

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"agrelha/internal/domain"
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

func (c *mockConsole) Execute(_ context.Context, cmd string) (string, error) {
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
	online, err := mgr.OnlinePlayers(context.Background())
	if err != nil || len(online) != 3 {
		t.Fatalf("OnlinePlayers failed: online=%v, err=%v", online, err)
	}
}

// Admission enforcement is a decision agrelha records, not a question it asks
// the server: the answer has to survive a restart and be renderable into a
// world's environment, and a world with no console had no answer at all.
func TestAccessManagerWhitelistEnforced(t *testing.T) {
	ctx := context.Background()
	store := newMemAccessStore()
	audit := &mockAuditRecorder{}
	console := &mockConsole{}
	mgr := NewAccessManager(store, "access.yaml", console, WithAudit(audit))

	enforced, err := mgr.WhitelistEnforced(ctx)
	if err != nil || enforced {
		t.Fatalf("a world nobody has closed must start open: enforced=%v err=%v", enforced, err)
	}

	if err := mgr.SetWhitelistEnforced(ctx, true, "admin"); err != nil {
		t.Fatalf("SetWhitelistEnforced(true): %v", err)
	}
	if enforced, err := mgr.WhitelistEnforced(ctx); err != nil || !enforced {
		t.Errorf("after turning it on: enforced=%v err=%v", enforced, err)
	}
	declared, err := mgr.DeclaredAccess(ctx)
	if err != nil || declared.WhitelistEnforced == nil || !*declared.WhitelistEnforced {
		t.Errorf("the decision must be recorded for the renderer: %+v err=%v", declared, err)
	}
	if len(console.commands) != 1 || console.commands[0] != "/whitelist on" {
		t.Errorf("expected /whitelist on, got: %v", console.commands)
	}

	if err := mgr.SetWhitelistEnforced(ctx, false, "admin"); err != nil {
		t.Fatalf("SetWhitelistEnforced(false): %v", err)
	}
	if enforced, err := mgr.WhitelistEnforced(ctx); err != nil || enforced {
		t.Errorf("after turning it off: enforced=%v err=%v", enforced, err)
	}
	if len(console.commands) != 2 || console.commands[1] != "/whitelist off" {
		t.Errorf("expected /whitelist off, got: %v", console.commands)
	}
}

// Nothing but the switch may close a world: a whitelist full of names still
// leaves Admission undecided, so creating a world cannot lock anyone out.
func TestWhitelistedNamesDoNotEnforceAdmission(t *testing.T) {
	ctx := context.Background()
	mgr := NewAccessManager(newMemAccessStore(), "access.yaml", nil)

	if _, err := mgr.AddWhitelist(ctx, "steve"); err != nil {
		t.Fatalf("AddWhitelist: %v", err)
	}
	declared, err := mgr.DeclaredAccess(ctx)
	if err != nil {
		t.Fatalf("DeclaredAccess: %v", err)
	}
	if len(declared.Whitelist) != 1 {
		t.Errorf("whitelist = %v, want steve", declared.Whitelist)
	}
	if declared.WhitelistEnforced != nil {
		t.Errorf("admission became %v by adding a name; it must stay undecided", *declared.WhitelistEnforced)
	}
	if declared.Enforced() {
		t.Error("an undecided world reported itself as whitelist-only")
	}
}

// memAccessStore is the smallest StateStore that remembers, which is what the
// declared-state tests need and the failing fakes cannot give.
type memAccessStore struct {
	docs map[string]ports.Document
}

func newMemAccessStore() *memAccessStore {
	return &memAccessStore{docs: map[string]ports.Document{}}
}

func (m *memAccessStore) Get(_ context.Context, path string) (ports.Document, error) {
	doc, ok := m.docs[path]
	if !ok {
		return ports.Document{}, os.ErrNotExist
	}
	return doc, nil
}

func (m *memAccessStore) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	m.docs[path] = doc
	return nil
}

func (m *memAccessStore) PutTree(_ context.Context, _ string, _ map[string]ports.Document, _ string) error {
	return nil
}

func (m *memAccessStore) Delete(_ context.Context, path string, _ string) error {
	delete(m.docs, path)
	return nil
}

func (m *memAccessStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	doc := m.docs[path]
	changed, err := fn(&doc)
	if err != nil || !changed {
		return false, err
	}
	return true, m.Put(ctx, path, doc, msg)
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
	if enforced, err := nilMgr.WhitelistEnforced(context.Background()); err != nil || enforced {
		t.Errorf("with nothing configured Admission is simply undecided: enforced=%v err=%v", enforced, err)
	}
	if err := nilMgr.SetWhitelistEnforced(context.Background(), true); err == nil {
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

	// 5. WhitelistEnforced surfaces a store that cannot be read
	readErrMgr := NewAccessManager(&failingAccessStore{getErr: errors.New("boom")}, "path", nil)
	if _, err := readErrMgr.WhitelistEnforced(context.Background()); err == nil {
		t.Errorf("expected the store read error to surface")
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
	// Recorded is enough to keep the switch, but the caller has to be told the
	// server has not heard it yet - saying "done" would be the lie this page
	// used to tell.
	err := errMgr.SetWhitelistEnforced(context.Background(), true, "")
	if !errors.Is(err, ErrRecordedOnly) {
		t.Errorf("err = %v, want ErrRecordedOnly: the switch was recorded, the server was not told", err)
	}
	// GrantOp with empty actor
	_, _ = errMgr.GrantOp(ctx, "playerNoActor")
	if len(auditRec.entries) == 0 || auditRec.entries[len(auditRec.entries)-1] != "-:mc-op-grant:playerNoActor" {
		t.Errorf("expected '-' actor, got: %v", auditRec.entries)
	}
}

func TestReadJSONUsers(t *testing.T) {
	// 1. Missing file returns nil, nil
	users, err := ReadJSONUsers("non-existent-path.json")
	if err != nil || users != nil {
		t.Errorf("expected nil, nil for non-existent file, got %v, %v", users, err)
	}

	// 2. Valid whitelist.json or ops.json
	tmp := t.TempDir()
	wlFile := tmp + "/whitelist.json"
	content := `[
		{"uuid": "1111-2222", "name": "notch"},
		{"uuid": "3333-4444", "name": "jeb_"},
		{"uuid": "5555-6666", "name": "   "}
	]`
	if err := os.WriteFile(wlFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	users, err = ReadJSONUsers(wlFile)
	if err != nil {
		t.Fatalf("unexpected error reading json users: %v", err)
	}
	if len(users) != 2 || users[0] != "notch" || users[1] != "jeb_" {
		t.Errorf("unexpected users: %v", users)
	}
}

func TestAccessManagerStandaloneRCON(t *testing.T) {
	ctx := context.Background()
	console := &mockConsole{response: "Added player1 to the whitelist"}
	audit := &mockAuditRecorder{}

	// Standalone AccessManager with RCON and no StateStore (e.g. Docker mode)
	mgr := NewAccessManager(nil, "", console, WithAudit(audit))

	// 1. AddWhitelist via RCON
	changed, err := mgr.AddWhitelist(ctx, "player1", "admin")
	if err != nil || !changed {
		t.Fatalf("AddWhitelist via RCON failed: changed=%v, err=%v", changed, err)
	}
	if len(console.commands) < 2 || console.commands[0] != "/whitelist add player1" {
		t.Errorf("expected RCON /whitelist add player1, got: %v", console.commands)
	}

	// 2. RemoveWhitelist via RCON
	console.commands = nil
	console.response = "Removed player1 from the whitelist"
	changed, err = mgr.RemoveWhitelist(ctx, "player1", "admin")
	if err != nil || !changed {
		t.Fatalf("RemoveWhitelist via RCON failed: changed=%v, err=%v", changed, err)
	}
	if len(console.commands) < 2 || console.commands[0] != "/whitelist remove player1" {
		t.Errorf("expected RCON /whitelist remove player1, got: %v", console.commands)
	}

	// 3. GrantOp via RCON
	console.commands = nil
	console.response = "Made player1 a server operator"
	changed, err = mgr.GrantOp(ctx, "player1", "admin")
	if err != nil || !changed {
		t.Fatalf("GrantOp via RCON failed: changed=%v, err=%v", changed, err)
	}
	if len(console.commands) != 1 || console.commands[0] != "/op player1" {
		t.Errorf("expected RCON /op player1, got: %v", console.commands)
	}

	// 4. RevokeOp via RCON
	console.commands = nil
	console.response = "Made player1 no longer a server operator"
	changed, err = mgr.RevokeOp(ctx, "player1", "admin")
	if err != nil || !changed {
		t.Fatalf("RevokeOp via RCON failed: changed=%v, err=%v", changed, err)
	}
	if len(console.commands) != 1 || console.commands[0] != "/deop player1" {
		t.Errorf("expected RCON /deop player1, got: %v", console.commands)
	}

	// 5. ListAccess queries /whitelist list via RCON
	console.commands = nil
	console.response = "There are 2 whitelisted players: notch, jeb_"
	ops, wl, err := mgr.ListAccess(ctx)
	if err != nil {
		t.Fatalf("ListAccess via RCON failed: %v", err)
	}
	if len(wl) != 2 || wl[0] != "notch" || wl[1] != "jeb_" {
		t.Errorf("expected 2 whitelisted players from RCON, got: %v", wl)
	}
	_ = ops
}

func TestAccessManager_UsernameValidation(t *testing.T) {
	ctx := context.Background()

	invalidUsernames := []struct {
		name     string
		username string
	}{
		{name: "newline injection", username: "steve\n/op malicious"},
		{name: "semicolon / commands", username: "player;kick all"},
		{name: "too long (>16 chars)", username: "player_with_a_name_that_is_far_too_long"},
		{name: "spaces", username: "user name"},
		{name: "symbols", username: "steve!"},
		{name: "empty", username: ""},
	}

	for _, tt := range invalidUsernames {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockStateStore{
				docs: map[string]ports.Document{
					"manifests/neoforge-access.yaml": {
						Data: map[string]string{
							"ops.txt":       "",
							"whitelist.txt": "",
						},
					},
				},
			}
			console := &mockConsole{}
			mgr := NewAccessManager(store, "manifests/neoforge-access.yaml", console)

			// GrantOp
			if _, err := mgr.GrantOp(ctx, tt.username, "admin"); !errors.Is(err, domain.ErrInvalidMinecraftUsername) {
				t.Errorf("GrantOp(%q) error = %v, want %v", tt.username, err, domain.ErrInvalidMinecraftUsername)
			}
			// RevokeOp
			if _, err := mgr.RevokeOp(ctx, tt.username, "admin"); !errors.Is(err, domain.ErrInvalidMinecraftUsername) {
				t.Errorf("RevokeOp(%q) error = %v, want %v", tt.username, err, domain.ErrInvalidMinecraftUsername)
			}
			// AddWhitelist
			if _, err := mgr.AddWhitelist(ctx, tt.username, "admin"); !errors.Is(err, domain.ErrInvalidMinecraftUsername) {
				t.Errorf("AddWhitelist(%q) error = %v, want %v", tt.username, err, domain.ErrInvalidMinecraftUsername)
			}
			// RemoveWhitelist
			if _, err := mgr.RemoveWhitelist(ctx, tt.username, "admin"); !errors.Is(err, domain.ErrInvalidMinecraftUsername) {
				t.Errorf("RemoveWhitelist(%q) error = %v, want %v", tt.username, err, domain.ErrInvalidMinecraftUsername)
			}

			// Ensure neither store was modified nor console commands were executed
			if len(console.commands) != 0 {
				t.Errorf("expected 0 console commands, got: %v", console.commands)
			}
			if store.docs["manifests/neoforge-access.yaml"].Data["ops.txt"] != "" {
				t.Errorf("expected ops.txt unchanged, got: %q", store.docs["manifests/neoforge-access.yaml"].Data["ops.txt"])
			}
			if store.docs["manifests/neoforge-access.yaml"].Data["whitelist.txt"] != "" {
				t.Errorf("expected whitelist.txt unchanged, got: %q", store.docs["manifests/neoforge-access.yaml"].Data["whitelist.txt"])
			}
		})
	}
}
