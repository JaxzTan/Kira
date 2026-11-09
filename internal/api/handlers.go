package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/JaxzTan/kira/internal/auth"
	"github.com/JaxzTan/kira/internal/export"
	"github.com/JaxzTan/kira/internal/ledger"
)

// ---- me ----

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok { // Middleware always sets it; defensive.
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "not signed in", nil)
		return
	}
	writeJSON(w, http.StatusOK, Me{Email: u.Email, Name: u.Name, Picture: u.Picture})
}

// ---- entries ----

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, errBadRequest("%s must be a positive integer", name)
	}
	return id, nil
}

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ledger.Filter{From: q.Get("from"), To: q.Get("to"), Method: q.Get("method"), Q: q.Get("q"), Status: q.Get("status")}
	if c := q.Get("category"); c != "" {
		id, err := strconv.ParseInt(c, 10, 64)
		if err != nil || id <= 0 {
			s.fail(w, r, errBadRequest("category must be a positive integer"))
			return
		}
		f.CategoryID = id
	}
	res, err := s.ledger.List(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, EntryList{Entries: toEntries(res.Entries), TotalSen: res.TotalSen, Count: res.Count, VoidedCount: res.VoidedCount})
}

func (s *Server) handleGetEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	e, err := s.ledger.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toEntry(e))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	e, rows, err := s.ledger.History(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := EntryHistory{Entry: toEntry(e), Rows: make([]LedgerRow, 0, len(rows))}
	for _, row := range rows {
		out.Rows = append(out.Rows, toRow(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	var in EntryInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.appended(w, r)(s.ledger.Create(r.Context(), toInput(in)))
}

func (s *Server) handleAmend(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in EntryInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.appended(w, r)(s.ledger.Amend(r.Context(), id, toInput(in)))
}

func (s *Server) handleVoid(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in VoidInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.appended(w, r)(s.ledger.Void(r.Context(), id, in.Reason))
}

// appended writes the 201 for a committed append and advances DATA_DIR/head.
func (s *Server) appended(w http.ResponseWriter, r *http.Request) func(ledger.AppendResult, error) {
	return func(res ledger.AppendResult, err error) {
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if wrote, herr := s.head.Advance(res.Row); herr != nil {
			s.log.Error("could not record ledger head", "err", herr)
		} else if !wrote {
			s.log.Info("ledger head file not advanced (non-contiguous append); next verify records it", "id", res.Row.ID)
		}
		writeJSON(w, http.StatusCreated, toAppendResult(res))
	}
}

// ---- receipts ----

func (s *Server) handleUploadReceipt(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		s.fail(w, r, errBadRequest("malformed multipart body"))
		return
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			s.fail(w, r, errValidation(map[string]string{"file": "is required"}))
			return
		}
		if err != nil {
			s.fail(w, r, multipartErr(err))
			return
		}
		if part.FormName() != "file" {
			_, _ = io.Copy(io.Discard, part)
			part.Close()
			continue
		}
		info, err := s.receipts.Save(part, -1)
		part.Close()
		if err != nil {
			s.fail(w, r, multipartErr(err))
			return
		}
		writeJSON(w, http.StatusCreated, toReceipt(info))
		return
	}
}

func multipartErr(err error) error {
	if status, _, _, _ := classify(err); status != http.StatusInternalServerError {
		return err // size limit, unsupported type, ...
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "multipart") {
		return errBadRequest("malformed multipart body")
	}
	return err
}

func (s *Server) handleGetReceipt(w http.ResponseWriter, r *http.Request) {
	f, info, err := s.receipts.Open(chi.URLParam(r, "sha"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", info.ContentType)
	h.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, info.FileName()))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("Content-Security-Policy", receiptCSP)
	http.ServeContent(w, r, info.FileName(), fi.ModTime(), f)
}

// ---- categories ----

func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	inc := false
	switch v := r.URL.Query().Get("include_archived"); v {
	case "", "0", "false":
	case "1", "true":
		inc = true
	default:
		s.fail(w, r, errBadRequest("include_archived must be 1 or 0"))
		return
	}
	cats, err := s.store.ListCategories(r.Context(), inc)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := CategoryList{Categories: make([]Category, 0, len(cats))}
	for _, c := range cats {
		out.Categories = append(out.Categories, toCategory(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var in CategoryCreate
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.store.CreateCategory(r.Context(), in.Name, in.Color)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCategory(c))
}

func (s *Server) handlePatchCategory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in CategoryPatch
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Name == nil && in.Archived == nil {
		s.fail(w, r, errBadRequest("nothing to update: send name and/or archived"))
		return
	}
	ctx := r.Context()
	c, err := s.store.GetCategory(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Name != nil {
		if c, err = s.store.RenameCategory(ctx, id, *in.Name); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.Archived != nil {
		if *in.Archived {
			c, err = s.store.ArchiveCategory(ctx, id, s.now())
		} else {
			c, err = s.store.UnarchiveCategory(ctx, id)
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toCategory(c))
}

// ---- summary & export ----

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.ledger.Summary(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSummary(sum))
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, format := q.Get("from"), q.Get("to"), q.Get("format")
	if from == "" || to == "" {
		s.fail(w, r, errBadRequest("from and to are required (YYYY-MM-DD)"))
		return
	}
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "zip" {
		s.fail(w, r, errBadRequest("format must be csv or zip"))
		return
	}
	rows, err := s.ledger.ExportEntries(r.Context(), from, to)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if from > to {
		s.fail(w, r, errBadRequest("from must not be after to"))
		return
	}
	name := "kira-" + from + "_" + to
	if format == "csv" {
		var buf bytes.Buffer
		if err := export.WriteCSV(&buf, rows, s.receipts); err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(buf.Bytes())
		return
	}
	// Zip is streamed (receipts can be large); an error mid-stream can only be logged.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := export.WriteZip(w, rows, s.receipts); err != nil {
		s.log.Error("zip export failed mid-stream", "err", err)
	}
}

// ---- ledger ----

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	res, err := VerifyRecorded(r.Context(), s.ledger, s.head, s.log)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toVerifyResult(res, s.lastBackup()))
}

// handleAcknowledge re-enables writes after a failed verify (TR-7). If no
// verify has run in this process yet, one runs first. After acknowledging a
// broken ledger the current head is recorded, so an acknowledged tail
// deletion is not reported again on the next verify (ledger docs).
func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	if _, ran := s.ledger.LastVerify(); !ran {
		if _, err := VerifyRecorded(r.Context(), s.ledger, s.head, s.log); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	res, _ := s.ledger.Acknowledge()
	if !res.OK {
		hd := ledger.Head{ID: res.HeadID, Hash: res.HeadHash}
		if err := s.head.Write(hd); err != nil {
			s.log.Error("could not record ledger head", "err", err)
		}
		s.log.Warn("broken ledger acknowledged by owner; writes re-enabled", "head", FormatHead(hd),
			"broken_at", derefInt(res.BrokenAt), "reason", derefStr(res.Reason), "at", s.now().UTC().Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, toVerifyResult(res, s.lastBackup()))
}

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
