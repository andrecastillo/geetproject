package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dv310p3r/geet/internal/store"
)

type Server struct {
	st  *store.Store
	web http.Handler
}

// New wires the JSON API and hands anything unmatched to the web handler, so the
// SPA and the API share one port and therefore one container.
func New(st *store.Store, web http.Handler) http.Handler {
	s := &Server{st: st, web: web}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)

	mux.HandleFunc("GET /api/statuses", s.listStatuses)

	mux.HandleFunc("GET /api/labels", s.listLabels)
	mux.HandleFunc("POST /api/labels", s.createLabel)
	mux.HandleFunc("PATCH /api/labels/{id}", s.updateLabel)
	mux.HandleFunc("DELETE /api/labels/{id}", s.deleteLabel)

	mux.HandleFunc("GET /api/boards", s.listBoards)
	mux.HandleFunc("POST /api/boards", s.createBoard)
	mux.HandleFunc("GET /api/boards/{slug}", s.getBoard)
	mux.HandleFunc("PATCH /api/boards/{slug}", s.updateBoard)
	mux.HandleFunc("DELETE /api/boards/{slug}", s.deleteBoard)
	mux.HandleFunc("PUT /api/boards/{slug}/columns", s.setBoardColumns)

	mux.HandleFunc("GET /api/tickets", s.listTickets)
	mux.HandleFunc("POST /api/tickets", s.createTicket)
	mux.HandleFunc("GET /api/tickets/{key}", s.getTicket)
	mux.HandleFunc("PATCH /api/tickets/{key}", s.updateTicket)
	mux.HandleFunc("DELETE /api/tickets/{key}", s.deleteTicket)
	mux.HandleFunc("POST /api/tickets/{key}/move", s.moveTicket)

	mux.HandleFunc("GET /api/tickets/{key}/comments", s.listComments)
	mux.HandleFunc("POST /api/tickets/{key}/comments", s.createComment)
	mux.HandleFunc("PATCH /api/comments/{id}", s.updateComment)
	mux.HandleFunc("DELETE /api/comments/{id}", s.deleteComment)

	if web != nil {
		mux.Handle("/", web)
	}
	return mux
}

// ---- plumbing ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

// writeErr maps the store's sentinel errors onto status codes in one place, so
// handlers can just pass errors up.
func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, store.ErrInvalid):
		code = http.StatusBadRequest
	case errors.Is(err, store.ErrConflict):
		code = http.StatusConflict
	}
	if code == http.StatusInternalServerError {
		log.Printf("server error: %v", err)
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathInt(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- statuses ----

func (s *Server) listStatuses(w http.ResponseWriter, r *http.Request) {
	out, err := s.st.ListStatuses(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- labels ----

func (s *Server) listLabels(w http.ResponseWriter, r *http.Request) {
	out, err := s.st.ListLabels(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decode(w, r, &req) {
		return
	}
	l, err := s.st.CreateLabel(r.Context(), req.Name, req.Color)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) updateLabel(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		badRequest(w, "label id must be a number")
		return
	}
	var req struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if !decode(w, r, &req) {
		return
	}
	l, err := s.st.UpdateLabel(r.Context(), id, req.Name, req.Color)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) deleteLabel(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		badRequest(w, "label id must be a number")
		return
	}
	if err := s.st.DeleteLabel(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

// ---- boards ----

func (s *Server) listBoards(w http.ResponseWriter, r *http.Request) {
	out, err := s.st.ListBoards(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createBoard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string  `json:"name"`
		Slug            string  `json:"slug"`
		FilterType      string  `json:"filter_type"`
		FilterLabelMode string  `json:"filter_label_mode"`
		FilterLabelIDs  []int64 `json:"filter_label_ids"`
		StatusIDs       []int64 `json:"status_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	labels := make([]store.Label, 0, len(req.FilterLabelIDs))
	for _, id := range req.FilterLabelIDs {
		labels = append(labels, store.Label{ID: id})
	}
	b, err := s.st.CreateBoard(r.Context(), store.Board{
		Name: req.Name, Slug: req.Slug, FilterType: req.FilterType,
		FilterLabelMode: req.FilterLabelMode, FilterLabels: labels,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	// A board with no columns would render as a blank page, so default to
	// every status unless the caller picked a set.
	ids := req.StatusIDs
	if len(ids) == 0 {
		statuses, err := s.st.ListStatuses(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		for _, st := range statuses {
			ids = append(ids, st.ID)
		}
	}
	if err := s.st.SetBoardColumns(r.Context(), b.ID, ids); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	view, err := s.st.GetBoard(r.Context(), r.PathValue("slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) updateBoard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            *string  `json:"name"`
		FilterType      *string  `json:"filter_type"`
		FilterLabelMode *string  `json:"filter_label_mode"`
		FilterLabelIDs  *[]int64 `json:"filter_label_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := s.st.UpdateBoard(r.Context(), r.PathValue("slug"), store.BoardPatch{
		Name: req.Name, FilterType: req.FilterType,
		FilterLabelMode: req.FilterLabelMode, FilterLabelIDs: req.FilterLabelIDs,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) deleteBoard(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteBoard(r.Context(), r.PathValue("slug")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) setBoardColumns(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StatusIDs []int64 `json:"status_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := s.st.GetBoardBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.st.SetBoardColumns(r.Context(), b.ID, req.StatusIDs); err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.st.GetBoard(r.Context(), b.Slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// ---- tickets ----

func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TicketFilter{
		Type:      q.Get("type"),
		LabelMode: q.Get("label_mode"),
		Search:    q.Get("q"),
	}
	if v := q.Get("status"); v != "" {
		statuses, err := s.st.ListStatuses(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		for _, st := range statuses {
			if st.Slug == v {
				f.StatusID = st.ID
			}
		}
		if f.StatusID == 0 {
			badRequest(w, "unknown status "+v)
			return
		}
	}
	if v := q.Get("parent"); v != "" {
		p, err := s.st.GetTicket(r.Context(), v)
		if err != nil {
			writeErr(w, err)
			return
		}
		f.ParentID = &p.ID
	}
	if v := q.Get("label_ids"); v != "" {
		for _, part := range strings.Split(v, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil {
				badRequest(w, "label_ids must be comma-separated numbers")
				return
			}
			f.LabelIDs = append(f.LabelIDs, id)
		}
	}
	out, err := s.st.ListTickets(r.Context(), f)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type        string  `json:"type"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Status      string  `json:"status"`
		Parent      string  `json:"parent"`
		LabelIDs    []int64 `json:"label_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	t, err := s.st.CreateTicket(r.Context(), store.NewTicket{
		Type: req.Type, Title: req.Title, Description: req.Description,
		StatusSlug: req.Status, ParentKey: req.Parent, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// ticketDetail is one round trip for the detail view: the ticket, its children
// and its comments.
type ticketDetail struct {
	*store.Ticket
	Children []store.Ticket  `json:"children"`
	Comments []store.Comment `json:"comments"`
}

func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	t, err := s.st.GetTicket(r.Context(), key)
	if err != nil {
		writeErr(w, err)
		return
	}
	children, err := s.st.Children(r.Context(), key)
	if err != nil {
		writeErr(w, err)
		return
	}
	comments, err := s.st.ListComments(r.Context(), key)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ticketDetail{Ticket: t, Children: children, Comments: comments})
}

func (s *Server) updateTicket(w http.ResponseWriter, r *http.Request) {
	// Every field is a pointer so an absent key means "leave it alone" rather
	// than "set it to the zero value" - a PATCH must never blank what it did
	// not mention.
	var req struct {
		Title       *string  `json:"title"`
		Description *string  `json:"description"`
		Type        *string  `json:"type"`
		Status      *string  `json:"status"`
		Parent      *string  `json:"parent"`
		LabelIDs    *[]int64 `json:"label_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	t, err := s.st.UpdateTicket(r.Context(), r.PathValue("key"), store.TicketPatch{
		Title: req.Title, Description: req.Description, Type: req.Type,
		StatusSlug: req.Status, ParentKey: req.Parent, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTicket(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteTicket(r.Context(), r.PathValue("key")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) moveTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Board  string `json:"board"`
		Status string `json:"status"`
		After  string `json:"after"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Board == "" {
		badRequest(w, "board is required")
		return
	}
	view, err := s.st.MoveCard(r.Context(), req.Board, r.PathValue("key"), req.Status, req.After)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// ---- comments ----

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	out, err := s.st.ListComments(r.Context(), r.PathValue("key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.st.CreateComment(r.Context(), r.PathValue("key"), req.Body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		badRequest(w, "comment id must be a number")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := s.st.UpdateComment(r.Context(), id, req.Body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		badRequest(w, "comment id must be a number")
		return
	}
	if err := s.st.DeleteComment(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
