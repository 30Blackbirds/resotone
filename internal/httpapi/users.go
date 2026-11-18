package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/30blackbirds/resotone/internal/auth"
	"github.com/30blackbirds/resotone/internal/users"
)

// userTarget resolves the tenant (from the verified actor, never from the
// request) and the {id} path value. It writes the refusal itself.
func (h *Handlers) userTarget(w http.ResponseWriter, r *http.Request) (tenant, id uuid.UUID, ok bool) {
	tenant, ok = h.tenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	if h.userAdmin == nil {
		writeJSON(w, http.StatusNotImplemented, errBody("user store not configured"))
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid user id"))
		return uuid.Nil, uuid.Nil, false
	}
	return tenant, id, true
}

// userError maps the store's answers; anything else is a 500 that says nothing.
func (h *Handlers) userError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, users.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody("no such user"))
	case errors.Is(err, users.ErrLastAdmin):
		writeJSON(w, http.StatusConflict, errBody("this is the last active admin of the tenant"))
	case errors.Is(err, users.ErrErased):
		writeJSON(w, http.StatusConflict, errBody("this user's data has been erased"))
	default:
		if h.log != nil {
			h.log.Error("users_api_failed", "path", r.URL.Path, "err", err)
		}
		writeJSON(w, http.StatusInternalServerError, errBody("internal error"))
	}
}

// audit records who changed whom. User ids only: the log carries no name,
// email or subject.
func (h *Handlers) audit(r *http.Request, event string, target uuid.UUID, kv ...any) {
	if h.log == nil {
		return
	}
	actor, _ := auth.ActorFromContext(r.Context())
	h.log.Info(event, append([]any{"tenant", actor.Tenant, "by", actor.UserID.String(), "user", target.String()}, kv...)...)
}

func (h *Handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.tenant(w, r)
	if !ok {
		return
	}
	if h.userAdmin == nil {
		writeJSON(w, http.StatusNotImplemented, errBody("user store not configured"))
		return
	}
	list, err := h.userAdmin.List(r.Context(), tenant)
	if err != nil {
		h.userError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}

func (h *Handlers) getUser(w http.ResponseWriter, r *http.Request) {
	tenant, id, ok := h.userTarget(w, r)
	if !ok {
		return
	}
	u, err := h.userAdmin.Get(r.Context(), tenant, id)
	if err != nil {
		h.userError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, u)
}

// exportUser is the user's data as Resotone holds it, as a download: the record
// and the role. Content carries no author today, so there is nothing else to
// include; the README's privacy section says so.
func (h *Handlers) exportUser(w http.ResponseWriter, r *http.Request) {
	tenant, id, ok := h.userTarget(w, r)
	if !ok {
		return
	}
	u, err := h.userAdmin.Get(r.Context(), tenant, id)
	if err != nil {
		h.userError(w, r, err)
		return
	}
	h.audit(r, "user_exported", id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="user-`+id.String()+`.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (h *Handlers) putUserRole(w http.ResponseWriter, r *http.Request) {
	tenant, id, ok := h.userTarget(w, r)
	if !ok {
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	role, known := auth.ParseRole(in.Role)
	if !known {
		writeJSON(w, http.StatusBadRequest, errBody("role must be one of viewer, editor, publisher, admin"))
		return
	}
	if err := h.userAdmin.SetRole(r.Context(), tenant, id, role); err != nil {
		h.userError(w, r, err)
		return
	}
	h.audit(r, "user_role_set", id, "role", role.String())
	u, err := h.userAdmin.Get(r.Context(), tenant, id)
	if err != nil {
		h.userError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handlers) setUserDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	tenant, id, ok := h.userTarget(w, r)
	if !ok {
		return
	}
	if err := h.userAdmin.SetDisabled(r.Context(), tenant, id, disabled); err != nil {
		h.userError(w, r, err)
		return
	}
	event := "user_enabled"
	if disabled {
		event = "user_disabled"
	}
	h.audit(r, event, id)
	u, err := h.userAdmin.Get(r.Context(), tenant, id)
	if err != nil {
		h.userError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handlers) disableUser(w http.ResponseWriter, r *http.Request) { h.setUserDisabled(w, r, true) }

func (h *Handlers) enableUser(w http.ResponseWriter, r *http.Request) { h.setUserDisabled(w, r, false) }

func (h *Handlers) eraseUser(w http.ResponseWriter, r *http.Request) {
	tenant, id, ok := h.userTarget(w, r)
	if !ok {
		return
	}
	if err := h.userAdmin.Erase(r.Context(), tenant, id); err != nil {
		h.userError(w, r, err)
		return
	}
	h.audit(r, "user_erased", id)
	w.WriteHeader(http.StatusNoContent)
}
