package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/render"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// fail maps domain errors to HTTP responses; unknown errors are logged, not leaked.
func fail(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var ve *domain.ValidationError
	var ce *domain.ConflictError
	var le *domain.LockedError
	var re *render.CompileError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusUnprocessableEntity, "validation", ve.Message)
	case errors.As(err, &re):
		writeError(w, http.StatusUnprocessableEntity, "render", re.Message)
	case errors.As(err, &le):
		writeError(w, http.StatusConflict, "locked", le.Message)
	case errors.As(err, &ce):
		writeError(w, http.StatusConflict, "conflict", ce.Message)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		log.Error("request failed", "error", err, "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}

const maxJSONBody = 1 << 20

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return uuid.Nil, false
	}
	return id, true
}

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}
