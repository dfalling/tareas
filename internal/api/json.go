package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// maxBodyBytes bounds request bodies: well above any legit todo payload,
// well below anything that could pressure the server.
const maxBodyBytes = 1 << 20 // 1 MiB

// writeJSON sends status with v as the JSON body. The encoder's trailing
// newline keeps logs and terminals tidy.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorJSON sends the API's single error shape: {"error": "..."}.
func errorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}

// unauthorized answers 401 with the challenge a 401 should carry.
func (a *App) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	errorJSON(w, http.StatusUnauthorized, "missing, invalid, or expired token")
}

// serverError logs and answers 500 without leaking internals.
func (a *App) serverError(w http.ResponseWriter, err error) {
	slog.Error("api server error", "err", err)
	errorJSON(w, http.StatusInternalServerError, "internal server error")
}

// readJSON decodes a single JSON object from the request body into dst,
// rejecting unknown fields (typos should fail loudly, not silently no-op),
// oversized bodies, and trailing content.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}

// badRequest maps readJSON failures to their message.
func badRequest(err error) string {
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr):
		return "request body too large"
	default:
		return "invalid JSON body"
	}
}
