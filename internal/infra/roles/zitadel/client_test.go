package zitadel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type capture struct {
	mu        sync.Mutex
	tokenHits int
	paths     []string
	bodies    map[string]map[string]any
	headers   map[string]http.Header
	respond   map[string]func(w http.ResponseWriter)
}

func newServer(t *testing.T) (*httptest.Server, *capture, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	serviceKey, err := json.Marshal(map[string]string{
		"type": "serviceaccount", "keyId": "k1", "userId": "u1", "key": string(pemBytes),
	})
	if err != nil {
		t.Fatal(err)
	}

	c := &capture{
		bodies:  map[string]map[string]any{},
		headers: map[string]http.Header{},
		respond: map[string]func(w http.ResponseWriter){},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if r.URL.Path == "/oauth/v2/token" {
			c.tokenHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok-123","expires_in":3600}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		c.paths = append(c.paths, r.URL.Path)
		c.bodies[r.URL.Path] = body
		c.headers[r.URL.Path] = r.Header.Clone()
		if fn, ok := c.respond[r.URL.Path]; ok {
			fn(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, c, serviceKey
}

func newClient(t *testing.T, srv *httptest.Server, key []byte) *Client {
	t.Helper()
	cl, err := New(Config{
		Issuer:     srv.URL,
		APIBaseURL: srv.URL,
		ProjectID:  "proj-1",
		ServiceKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

func TestAddRoleUsesProjectV2(t *testing.T) {
	srv, cap, key := newServer(t)
	cl := newClient(t, srv, key)

	if err := cl.AddRole(context.Background(), domain.InstanceRole(domain.GameValheim, 2), "Valheim 02 - Boppo"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}

	const want = "/zitadel.project.v2.ProjectService/AddProjectRole"
	body, ok := cap.bodies[want]
	if !ok {
		t.Fatalf("expected a call to %s, got %v", want, cap.paths)
	}
	if body["projectId"] != "proj-1" || body["roleKey"] != "agrelha-valheim-02" {
		t.Errorf("unexpected body: %v", body)
	}
	if got := cap.headers[want].Get("Connect-Protocol-Version"); got != "1" {
		t.Errorf("Connect-Protocol-Version = %q, want 1", got)
	}
	if got := cap.headers[want].Get("Authorization"); got != "Bearer tok-123" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestAddRoleAlreadyExists(t *testing.T) {
	srv, cap, key := newServer(t)
	cap.respond["/zitadel.project.v2.ProjectService/AddProjectRole"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"already_exists","message":"role already exists"}`))
	}
	cl := newClient(t, srv, key)

	err := cl.AddRole(context.Background(), domain.InstanceRole(domain.GameValheim, 2), "x")
	if !errors.Is(err, ports.ErrRoleExists) {
		t.Fatalf("want ErrRoleExists, got %v", err)
	}
}

func TestRemoveRoleIsIdempotent(t *testing.T) {
	srv, cap, key := newServer(t)
	cap.respond["/zitadel.project.v2.ProjectService/RemoveProjectRole"] = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"no such role"}`))
	}
	cl := newClient(t, srv, key)

	if err := cl.RemoveRole(context.Background(), domain.InstanceRole(domain.GameValheim, 9)); err != nil {
		t.Fatalf("removing an absent role must succeed, got %v", err)
	}
}

func TestGrantRoleCreatesAuthorizationWhenAbsent(t *testing.T) {
	srv, cap, key := newServer(t)
	cap.respond["/zitadel.authorization.v2.AuthorizationService/ListAuthorizations"] = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"authorizations":[]}`))
	}
	cl := newClient(t, srv, key)

	if err := cl.GrantRole(context.Background(), "user-7", domain.InstanceRole(domain.GameValheim, 2)); err != nil {
		t.Fatalf("GrantRole: %v", err)
	}

	list := cap.bodies["/zitadel.authorization.v2.AuthorizationService/ListAuthorizations"]
	filters, _ := list["filters"].([]any)
	if len(filters) != 2 {
		t.Fatalf("expected a user and a project filter, got %v", list)
	}

	create, ok := cap.bodies["/zitadel.authorization.v2.AuthorizationService/CreateAuthorization"]
	if !ok {
		t.Fatalf("expected CreateAuthorization, got %v", cap.paths)
	}
	if create["userId"] != "user-7" || create["projectId"] != "proj-1" {
		t.Errorf("unexpected create body: %v", create)
	}
	roles, _ := create["roleKeys"].([]any)
	if len(roles) != 1 || roles[0] != "agrelha-valheim-02" {
		t.Errorf("roleKeys = %v", create["roleKeys"])
	}
}

func TestGrantRoleUpdatesExistingAuthorization(t *testing.T) {
	srv, cap, key := newServer(t)
	cap.respond["/zitadel.authorization.v2.AuthorizationService/ListAuthorizations"] = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"authorizations":[{"id":"auth-1","roles":[{"key":"agrelha-user"}]}]}`))
	}
	cl := newClient(t, srv, key)

	if err := cl.GrantRole(context.Background(), "user-7", domain.InstanceRole(domain.GameValheim, 2)); err != nil {
		t.Fatalf("GrantRole: %v", err)
	}

	up, ok := cap.bodies["/zitadel.authorization.v2.AuthorizationService/UpdateAuthorization"]
	if !ok {
		t.Fatalf("expected UpdateAuthorization, got %v", cap.paths)
	}
	if up["id"] != "auth-1" {
		t.Errorf("id = %v", up["id"])
	}
	roles, _ := up["roleKeys"].([]any)
	if len(roles) != 2 {
		t.Errorf("existing roles must be preserved, got %v", up["roleKeys"])
	}
}

func TestRevokeLastRoleDeletesAuthorization(t *testing.T) {
	srv, cap, key := newServer(t)
	cap.respond["/zitadel.authorization.v2.AuthorizationService/ListAuthorizations"] = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"authorizations":[{"id":"auth-1","roles":[{"key":"agrelha-valheim-02"}]}]}`))
	}
	cl := newClient(t, srv, key)

	if err := cl.RevokeRole(context.Background(), "user-7", domain.InstanceRole(domain.GameValheim, 2)); err != nil {
		t.Fatalf("RevokeRole: %v", err)
	}
	del, ok := cap.bodies["/zitadel.authorization.v2.AuthorizationService/DeleteAuthorization"]
	if !ok {
		t.Fatalf("expected DeleteAuthorization, got %v", cap.paths)
	}
	if del["id"] != "auth-1" {
		t.Errorf("id = %v", del["id"])
	}
}

func TestRolesForReadsRoleKeys(t *testing.T) {
	srv, cap, key := newServer(t)
	cap.respond["/zitadel.authorization.v2.AuthorizationService/ListAuthorizations"] = func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"authorizations":[{"id":"a","roles":[{"key":"agrelha-admin"},{"key":"agrelha-valheim-02"}]}]}`))
	}
	cl := newClient(t, srv, key)

	roles, err := cl.RolesFor(context.Background(), "user-7")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 || roles[0] != domain.RoleAdmin {
		t.Errorf("roles = %v", roles)
	}
}

func TestAccessTokenIsCached(t *testing.T) {
	srv, cap, key := newServer(t)
	cl := newClient(t, srv, key)

	for range 3 {
		if err := cl.AddRole(context.Background(), domain.InstanceRole(domain.GameValheim, 2), "x"); err != nil {
			t.Fatal(err)
		}
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.tokenHits != 1 {
		t.Errorf("token endpoint hit %d times, want 1", cap.tokenHits)
	}
}
