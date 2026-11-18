package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/30blackbirds/resotone/internal/auth"
	"github.com/30blackbirds/resotone/internal/users"
)

// recordingUsers answers from a fixed result and records the tenant every call
// was scoped to.
type recordingUsers struct {
	err     error
	tenants []uuid.UUID
	roles   []auth.Role
	calls   []string
}

func (f *recordingUsers) note(call string, tenant uuid.UUID) {
	f.calls = append(f.calls, call)
	f.tenants = append(f.tenants, tenant)
}

func (f *recordingUsers) List(_ context.Context, t uuid.UUID) ([]users.User, error) {
	f.note("list", t)
	return []users.User{{ID: uuid.New(), Status: "active", Role: "admin"}}, f.err
}

func (f *recordingUsers) Get(_ context.Context, t, id uuid.UUID) (users.User, error) {
	f.note("get", t)
	return users.User{ID: id, Status: "active", Role: "editor"}, f.err
}

func (f *recordingUsers) SetRole(_ context.Context, t, _ uuid.UUID, r auth.Role) error {
	f.note("setrole", t)
	f.roles = append(f.roles, r)
	return f.err
}

func (f *recordingUsers) SetDisabled(_ context.Context, t, _ uuid.UUID, d bool) error {
	if d {
		f.note("disable", t)
	} else {
		f.note("enable", t)
	}
	return f.err
}

func (f *recordingUsers) Erase(_ context.Context, t, _ uuid.UUID) error {
	f.note("erase", t)
	return f.err
}

const userID = "00000000-0000-0000-0000-0000000000aa"

func userRequest(t *testing.T, store users.Admin, actor auth.Actor, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewRouter(Deps{UserAdmin: store, Verifier: fixedVerifier{actor}})
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var userRoutes = []struct{ method, path, body string }{
	{"GET", "/api/v1/users", ""},
	{"GET", "/api/v1/users/" + userID, ""},
	{"GET", "/api/v1/users/" + userID + "/export", ""},
	{"PUT", "/api/v1/users/" + userID + "/role", `{"role":"editor"}`},
	{"POST", "/api/v1/users/" + userID + "/disable", ""},
	{"POST", "/api/v1/users/" + userID + "/enable", ""},
	{"DELETE", "/api/v1/users/" + userID, ""},
}

// Only an admin gets in, and the store is never reached for anyone else.
func TestUserRoutesAreAdminOnly(t *testing.T) {
	for _, role := range []auth.Role{auth.RoleNone, auth.RoleViewer, auth.RoleEditor, auth.RolePublisher} {
		for _, rt := range userRoutes {
			store := &recordingUsers{}
			w := userRequest(t, store, auth.Actor{Sub: "u", Tenant: tenantA, Role: role}, rt.method, rt.path, rt.body)
			if w.Code != http.StatusForbidden || len(store.calls) != 0 {
				t.Errorf("%v %s %s = %d with %d store calls; want 403 and none", role, rt.method, rt.path, w.Code, len(store.calls))
			}
		}
	}
	for _, rt := range userRoutes { // positive control: the same routes work for an admin
		store := &recordingUsers{}
		w := userRequest(t, store, auth.Actor{Sub: "u", Tenant: tenantA, Role: auth.RoleAdmin}, rt.method, rt.path, rt.body)
		if w.Code >= 300 || len(store.calls) == 0 {
			t.Errorf("admin %s %s = %d with %d store calls; want success", rt.method, rt.path, w.Code, len(store.calls))
		}
	}
}

// The tenant is the verified actor's, whatever the request says.
func TestUserRoutesUseOnlyTheActorsTenant(t *testing.T) {
	want := uuid.MustParse(tenantA)
	for _, rt := range userRoutes {
		store := &recordingUsers{}
		h := NewRouter(Deps{UserAdmin: store, Verifier: fixedVerifier{auth.Actor{Sub: "u", Tenant: tenantA, Role: auth.RoleAdmin}}})
		r := httptest.NewRequest(rt.method, rt.path+"?tenant="+tenantB+"&tenantId="+tenantB, strings.NewReader(rt.body))
		r.Header.Set("Authorization", "Bearer x")
		r.Header.Set("X-Dev-Tenant", tenantB)
		r.Header.Set("X-Scope-OrgID", tenantB)
		h.ServeHTTP(httptest.NewRecorder(), r)
		if len(store.tenants) == 0 {
			t.Fatalf("%s %s never reached the store", rt.method, rt.path)
		}
		for _, got := range store.tenants {
			if got != want {
				t.Errorf("%s %s scoped to %s, want the actor's %s", rt.method, rt.path, got, want)
			}
		}
	}
}

func TestPutUserRoleValidatesTheRole(t *testing.T) {
	admin := auth.Actor{Sub: "u", Tenant: tenantA, Role: auth.RoleAdmin}
	for body, want := range map[string]auth.Role{
		`{"role":"viewer"}`: auth.RoleViewer, `{"role":"editor"}`: auth.RoleEditor,
		`{"role":"publisher"}`: auth.RolePublisher, `{"role":"admin"}`: auth.RoleAdmin,
	} {
		store := &recordingUsers{}
		w := userRequest(t, store, admin, "PUT", "/api/v1/users/"+userID+"/role", body)
		if w.Code != http.StatusOK || len(store.roles) != 1 || store.roles[0] != want {
			t.Errorf("%s = %d roles %v; want 200 and %v", body, w.Code, store.roles, want)
		}
	}
	for _, body := range []string{`{"role":"none"}`, `{"role":"owner"}`, `{"role":""}`, `{}`, `{"role":"admin","x":1}`, `nope`} {
		store := &recordingUsers{}
		w := userRequest(t, store, admin, "PUT", "/api/v1/users/"+userID+"/role", body)
		if w.Code != http.StatusBadRequest || len(store.calls) != 0 {
			t.Errorf("%s = %d with %d store calls; want 400 and none", body, w.Code, len(store.calls))
		}
	}
	if w := userRequest(t, &recordingUsers{}, admin, "PUT", "/api/v1/users/not-a-uuid/role", `{"role":"admin"}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d, want 400", w.Code)
	}
}

func TestUserRoutesMapStoreAnswers(t *testing.T) {
	admin := auth.Actor{Sub: "u", Tenant: tenantA, Role: auth.RoleAdmin}
	for err, want := range map[error]int{
		users.ErrNotFound:  http.StatusNotFound,
		users.ErrLastAdmin: http.StatusConflict,
		users.ErrErased:    http.StatusConflict,
	} {
		w := userRequest(t, &recordingUsers{err: err}, admin, "POST", "/api/v1/users/"+userID+"/disable", "")
		if w.Code != want {
			t.Errorf("%v = %d, want %d", err, w.Code, want)
		}
	}
}

func TestUserRoutesWithoutAStoreAre501(t *testing.T) {
	admin := auth.Actor{Sub: "u", Tenant: tenantA, Role: auth.RoleAdmin}
	for _, rt := range userRoutes {
		if w := userRequest(t, nil, admin, rt.method, rt.path, rt.body); w.Code != http.StatusNotImplemented {
			t.Errorf("%s %s = %d, want 501", rt.method, rt.path, w.Code)
		}
	}
}

func TestExportIsADownloadAndEraseIs204(t *testing.T) {
	admin := auth.Actor{Sub: "u", Tenant: tenantA, Role: auth.RoleAdmin}
	w := userRequest(t, &recordingUsers{}, admin, "GET", "/api/v1/users/"+userID+"/export", "")
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("export headers: %v", w.Header())
	}
	if w := userRequest(t, &recordingUsers{}, admin, "DELETE", "/api/v1/users/"+userID, ""); w.Code != http.StatusNoContent {
		t.Errorf("erase = %d, want 204", w.Code)
	}
}
