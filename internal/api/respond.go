package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
	"github.com/JaxzTan/kira/internal/store"
)

// httpError is an error with a status and contract code.
type httpError struct {
	status int
	code   string
	msg    string
	fields map[string]string
}

func (e *httpError) Error() string { return e.msg }

func errBadRequest(format string, a ...any) error {
	return &httpError{status: http.StatusBadRequest, code: CodeBadRequest, msg: fmt.Sprintf(format, a...)}
}

func errValidation(fields map[string]string) error {
	return &httpError{status: http.StatusUnprocessableEntity, code: CodeValidation, msg: "some fields are invalid", fields: fields}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, fields map[string]string) {
	if code != CodeValidation {
		fields = nil
	}
	writeJSON(w, status, ErrorResponse{Error: msg, Code: code, Fields: fields})
}

// classify maps package errors to (status, code, message, fields) per the
// PLAN.md Cross-lane mapping. Unknown errors are 500 with a generic message.
func classify(err error) (int, string, string, map[string]string) {
	var he *httpError
	var ve *ledger.ValidationError
	var ne *store.NameError
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &he):
		return he.status, he.code, he.msg, he.fields
	case errors.As(err, &ve):
		return http.StatusUnprocessableEntity, CodeValidation, "some fields are invalid", ve.Fields
	case errors.As(err, &ne):
		return http.StatusUnprocessableEntity, CodeValidation, "some fields are invalid", map[string]string{ne.Field: ne.Msg}
	case errors.Is(err, ledger.ErrDateConfirmRequired):
		return http.StatusUnprocessableEntity, CodeDateConfirmRequired, ledger.ErrDateConfirmRequired.Error(), nil
	case errors.Is(err, ledger.ErrNotFound):
		return http.StatusNotFound, CodeNotFound, "entry not found", nil
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, CodeNotFound, "category not found", nil
	case errors.Is(err, receipts.ErrNotFound):
		return http.StatusNotFound, CodeNotFound, "receipt not found", nil
	case errors.Is(err, ledger.ErrNotCreate):
		return http.StatusConflict, CodeConflict, ledger.ErrNotCreate.Error(), nil
	case errors.Is(err, ledger.ErrVoided):
		return http.StatusConflict, CodeConflict, "entry is already voided", nil
	case errors.Is(err, store.ErrDuplicateName):
		return http.StatusConflict, CodeConflict, store.ErrDuplicateName.Error(), nil
	case errors.Is(err, ledger.ErrLedgerBroken):
		return http.StatusLocked, CodeLedgerBroken, ledger.ErrLedgerBroken.Error(), nil
	case errors.Is(err, ledger.ErrBadRequest):
		return http.StatusBadRequest, CodeBadRequest, err.Error(), nil
	case errors.Is(err, receipts.ErrInvalidSHA):
		return http.StatusBadRequest, CodeBadRequest, receipts.ErrInvalidSHA.Error(), nil
	case errors.Is(err, receipts.ErrTooLarge), errors.As(err, &mbe):
		return http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "request body too large", nil
	case errors.Is(err, receipts.ErrUnsupported):
		return http.StatusUnsupportedMediaType, CodeUnsupportedMedia, receipts.ErrUnsupported.Error(), nil
	case errors.Is(err, receipts.ErrInvalidImage):
		return http.StatusUnsupportedMediaType, CodeUnsupportedMedia, receipts.ErrInvalidImage.Error(), nil
	}
	return http.StatusInternalServerError, CodeInternal, "internal error", nil
}

// fail writes err as a JSON error. 500s are logged with details (never the
// request body, cookies or tokens) and returned with a generic message.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg, fields := classify(err)
	if status >= 500 {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeError(w, status, code, msg, fields)
}

// decodeJSON strictly decodes a single JSON object into dst: unknown fields
// are rejected and trailing data is an error. Type mismatches become 422
// with the field named.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return jsonErr(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return err
		}
		return errBadRequest("request body must be a single JSON object")
	}
	return nil
}

func jsonErr(err error) error {
	var mbe *http.MaxBytesError
	var te *json.UnmarshalTypeError
	var se *json.SyntaxError
	switch {
	case errors.As(err, &mbe):
		return err
	case errors.As(err, &te):
		field := te.Field
		if field == "" {
			return errBadRequest("request body must be a JSON object")
		}
		return errValidation(map[string]string{field: "must be a " + jsonType(te.Type.Kind().String())})
	case errors.As(err, &se), errors.Is(err, io.ErrUnexpectedEOF):
		return errBadRequest("malformed JSON")
	case errors.Is(err, io.EOF):
		return errBadRequest("request body is empty")
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return errBadRequest("unknown field %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	return errBadRequest("malformed JSON")
}

func jsonType(kind string) string {
	switch kind {
	case "int", "int64", "int32", "uint", "uint64", "float64":
		return "number"
	case "bool":
		return "boolean"
	case "string":
		return "string"
	case "ptr":
		return "valid value"
	}
	return "valid value"
}
