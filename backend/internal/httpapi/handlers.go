package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/claviq/desk/backend/internal/auth"
	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/storage"
)

// ---------- auth ----------

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !a.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "cross-origin request rejected")
		return
	}
	u, err := a.Sessions.Login(r.Context(), in.Email, in.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	if err := a.Sessions.Issue(w, u); err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"id": u.ID, "email": u.Email}})
}

func (a *api) logout(w http.ResponseWriter, _ *http.Request) {
	a.Sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	s, err := a.Service.GetSettings(r.Context(), actorFrom(r.Context()))
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":   map[string]any{"id": u.ID, "email": u.Email, "role": u.Role},
		"tenant": map[string]any{"id": s.Tenant.ID, "name": s.Tenant.Name, "currency": s.Tenant.Currency},
	})
}

func (a *api) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := a.Sessions.ChangePassword(r.Context(), userFrom(r.Context()).ID, in.Current, in.Next)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnprocessableEntity, "validation", "current password is incorrect")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation", err.Error())
		return
	}
	_ = a.Sessions.Issue(w, u) // keep this device signed in; others are signed out
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) connections(w http.ResponseWriter, r *http.Request) {
	c, err := a.OAuth.Store().Connections(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	if c == nil {
		c = []auth.Connection{}
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *api) disconnect(w http.ResponseWriter, r *http.Request) {
	if err := a.OAuth.Store().Disconnect(r.Context(), userFrom(r.Context()).ID, r.PathValue("clientID")); err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- settings ----------

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.Service.GetSettings(r.Context(), actorFrom(r.Context()))
	respond(w, r, a, s, err)
}

func (a *api) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in domain.TenantInput
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Service.UpdateTenant(r.Context(), actorFrom(r.Context()), in)
	respond(w, r, a, t, err)
}

func (a *api) getLogo(w http.ResponseWriter, r *http.Request) {
	s, err := a.Service.GetSettings(r.Context(), actorFrom(r.Context()))
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	b, ext, err := a.Service.GetLogo(r.Context(), s.Tenant.LogoPath)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && b == nil) {
		writeError(w, http.StatusNotFound, "not_found", "no logo")
		return
	}
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	ct := map[string]string{"png": "image/png", "jpg": "image/jpeg", "svg": "image/svg+xml"}[ext]
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(b)
}

func readUpload(w http.ResponseWriter, r *http.Request, field string, max int64) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, max+1<<20)
	if err := r.ParseMultipartForm(max); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "upload too large or not multipart")
		return nil, false
	}
	f, _, err := r.FormFile(field)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "missing file field "+field)
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		writeError(w, http.StatusBadRequest, "bad_request", "file too large")
		return nil, false
	}
	return b, true
}

func (a *api) uploadLogo(w http.ResponseWriter, r *http.Request) {
	b, ok := readUpload(w, r, "file", 2<<20)
	if !ok {
		return
	}
	if err := a.Service.UploadLogo(r.Context(), actorFrom(r.Context()), b); err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) removeLogo(w http.ResponseWriter, r *http.Request) {
	if err := a.Service.RemoveLogo(r.Context(), actorFrom(r.Context())); err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func optionalID(r *http.Request) (*uuid.UUID, error) {
	if r.PathValue("id") == "" {
		return nil, nil
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, domain.ErrNotFound
	}
	return &id, nil
}

func (a *api) saveTaxRate(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	var in domain.TaxRateInput
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Service.SaveTaxRate(r.Context(), actorFrom(r.Context()), id, in)
	respond(w, r, a, t, err)
}

func (a *api) saveBankAccount(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	var in domain.BankAccountInput
	if !decode(w, r, &in) {
		return
	}
	b, err := a.Service.SaveBankAccount(r.Context(), actorFrom(r.Context()), id, in)
	respond(w, r, a, b, err)
}

func (a *api) updateSequence(w http.ResponseWriter, r *http.Request) {
	var in domain.SequenceInput
	if !decode(w, r, &in) {
		return
	}
	s, err := a.Service.UpdateSequence(r.Context(), actorFrom(r.Context()), r.PathValue("docType"), in)
	respond(w, r, a, s, err)
}

func (a *api) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	logs, err := a.Service.ListAudit(r.Context(), actorFrom(r.Context()), q.Get("entity"), q.Get("entity_id"), queryInt(r, "limit", 50))
	respond(w, r, a, logs, err)
}

// ---------- customers ----------

func (a *api) listCustomers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cs, err := a.Service.ListCustomers(r.Context(), actorFrom(r.Context()), domain.CustomerFilter{
		Query: q.Get("q"), IncludeArchived: q.Get("archived") == "1",
		Limit: queryInt(r, "limit", 50), Offset: queryInt(r, "offset", 0)})
	respond(w, r, a, cs, err)
}

func (a *api) createCustomer(w http.ResponseWriter, r *http.Request) {
	var in domain.CustomerInput
	if !decode(w, r, &in) {
		return
	}
	c, err := a.Service.CreateCustomer(r.Context(), actorFrom(r.Context()), in)
	respondCreated(w, r, a, c, err)
}

func (a *api) getCustomer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	c, err := a.Service.GetCustomer(r.Context(), actorFrom(r.Context()), id)
	respond(w, r, a, c, err)
}

func (a *api) updateCustomer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in domain.CustomerInput
	if !decode(w, r, &in) {
		return
	}
	c, err := a.Service.UpdateCustomer(r.Context(), actorFrom(r.Context()), id, in)
	respond(w, r, a, c, err)
}

func (a *api) archiveCustomer(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		c, err := a.Service.SetCustomerArchived(r.Context(), actorFrom(r.Context()), id, archived)
		respond(w, r, a, c, err)
	}
}

// ---------- templates & fonts ----------

func (a *api) listTemplates(w http.ResponseWriter, r *http.Request) {
	ts, err := a.Service.ListTemplates(r.Context(), actorFrom(r.Context()), r.URL.Query().Get("doc_type"), r.URL.Query().Get("archived") == "1")
	respond(w, r, a, ts, err)
}

func (a *api) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in domain.TemplateInput
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Service.CreateTemplate(r.Context(), actorFrom(r.Context()), in)
	respondCreated(w, r, a, t, err)
}

func (a *api) getTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	t, err := a.Service.GetTemplate(r.Context(), actorFrom(r.Context()), id)
	respond(w, r, a, t, err)
}

func (a *api) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in domain.TemplateInput
	if !decode(w, r, &in) {
		return
	}
	t, err := a.Service.UpdateTemplate(r.Context(), actorFrom(r.Context()), id, in)
	respond(w, r, a, t, err)
}

func (a *api) archiveTemplate(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if err := a.Service.SetTemplateArchived(r.Context(), actorFrom(r.Context()), id, archived); err != nil {
			fail(w, r, a.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) listTemplateVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	vs, err := a.Service.ListTemplateVersions(r.Context(), actorFrom(r.Context()), id)
	respond(w, r, a, vs, err)
}

func (a *api) getTemplateVersion(w http.ResponseWriter, r *http.Request) {
	vid, ok := pathID(w, r, "vid")
	if !ok {
		return
	}
	v, err := a.Service.GetTemplateVersion(r.Context(), actorFrom(r.Context()), vid)
	respond(w, r, a, v, err)
}

func (a *api) restoreTemplateVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	vid, ok := pathID(w, r, "vid")
	if !ok {
		return
	}
	t, err := a.Service.RestoreTemplateVersion(r.Context(), actorFrom(r.Context()), id, vid)
	respond(w, r, a, t, err)
}

func (a *api) previewTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var vid *uuid.UUID
	if v := r.URL.Query().Get("version_id"); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid version_id")
			return
		}
		vid = &parsed
	}
	l, err := a.Service.TemplatePreviewLinks(r.Context(), actorFrom(r.Context()), id, vid)
	respond(w, r, a, l, err)
}

func (a *api) listFonts(w http.ResponseWriter, r *http.Request) {
	f, err := a.Service.ListFonts(r.Context(), actorFrom(r.Context()))
	respond(w, r, a, f, err)
}

func (a *api) addGoogleFont(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Family string `json:"family"`
	}
	if !decode(w, r, &in) {
		return
	}
	f, err := a.Service.AddGoogleFont(r.Context(), actorFrom(r.Context()), in.Family)
	respondCreated(w, r, a, f, err)
}

func (a *api) uploadFont(w http.ResponseWriter, r *http.Request) {
	b, ok := readUpload(w, r, "file", 15<<20)
	if !ok {
		return
	}
	weight, _ := strconv.Atoi(r.FormValue("weight"))
	if weight == 0 {
		weight = 400
	}
	style := r.FormValue("style")
	if style == "" {
		style = "normal"
	}
	f, err := a.Service.UploadFont(r.Context(), actorFrom(r.Context()), r.FormValue("family"), weight, style, b, r.FormValue("licence"))
	respondCreated(w, r, a, f, err)
}

// ---------- documents ----------

func (a *api) listDocuments(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := domain.DocumentFilter{Query: q.Get("q"), Status: q.Get("status"),
			Limit: queryInt(r, "limit", 30), Offset: queryInt(r, "offset", 0)}
		if v := q.Get("customer_id"); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				f.CustomerID = &id
			}
		}
		if v := q.Get("estimate_id"); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				f.EstimateID = &id
			}
		}
		docs, err := a.Service.ListDocuments(r.Context(), actorFrom(r.Context()), kind, f)
		respond(w, r, a, docs, err)
	}
}

func (a *api) createDocument(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in domain.DocumentInput
		if !decode(w, r, &in) {
			return
		}
		d, err := a.Service.CreateDocument(r.Context(), actorFrom(r.Context()), kind, in)
		respondCreated(w, r, a, d, err)
	}
}

func (a *api) getDocument(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		d, err := a.Service.GetDocument(r.Context(), actorFrom(r.Context()), kind, id)
		respond(w, r, a, d, err)
	}
}

func (a *api) updateDocument(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var in domain.DocumentInput
		if !decode(w, r, &in) {
			return
		}
		d, err := a.Service.UpdateDocument(r.Context(), actorFrom(r.Context()), kind, id, in)
		respond(w, r, a, d, err)
	}
}

func (a *api) deleteDocument(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if err := a.Service.DeleteDraft(r.Context(), actorFrom(r.Context()), kind, id); err != nil {
			fail(w, r, a.Logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) finaliseDocument(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		d, err := a.Service.Finalise(r.Context(), actorFrom(r.Context()), kind, id)
		respond(w, r, a, d, err)
	}
}

func (a *api) reviseDocument(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		d, err := a.Service.Revise(r.Context(), actorFrom(r.Context()), kind, id)
		respondCreated(w, r, a, d, err)
	}
}

func (a *api) documentLinks(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		l, err := a.Service.DocumentLinks(r.Context(), actorFrom(r.Context()), kind, id)
		respond(w, r, a, l, err)
	}
}

func (a *api) documentPDF(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		b, name, err := a.Service.DocumentPDF(r.Context(), actorFrom(r.Context()), kind, id)
		if err != nil {
			fail(w, r, a.Logger, err)
			return
		}
		writePDF(w, b, name)
	}
}

func (a *api) convertEstimate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in domain.ConvertInput
	if !decode(w, r, &in) {
		return
	}
	docs, err := a.Service.ConvertToInvoices(r.Context(), actorFrom(r.Context()), id, in)
	respondCreated(w, r, a, docs, err)
}

// ---------- public previews ----------

func (a *api) previewInfo(w http.ResponseWriter, r *http.Request) {
	info, err := a.Service.PreviewInfo(r.Context(), r.PathValue("token"))
	respond(w, r, a, info, err)
}

func (a *api) previewPage(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("n"), ".png"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	b, err := a.Service.PreviewPage(r.Context(), r.PathValue("token"), n)
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(b)
}

func (a *api) previewPDF(w http.ResponseWriter, r *http.Request) {
	b, name, err := a.Service.PreviewPDF(r.Context(), r.PathValue("token"))
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	writePDF(w, b, name)
}

func writePDF(w http.ResponseWriter, b []byte, name string) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(b)
}

// ---------- helpers ----------

func respond(w http.ResponseWriter, r *http.Request, a *api, v any, err error) {
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func respondCreated(w http.ResponseWriter, r *http.Request, a *api, v any, err error) {
	if err != nil {
		fail(w, r, a.Logger, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
