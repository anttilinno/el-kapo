package server

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

const sidCookie = "kapo_sid"

// Server is the table registry and HTTP routing layer.
type Server struct {
	mu     sync.Mutex
	tables map[string]*Table
	log    *slog.Logger
}

func New(log *slog.Logger) *Server {
	return &Server{tables: make(map[string]*Table), log: log}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleLobby)
	mux.HandleFunc("POST /games", s.handleCreate)
	mux.HandleFunc("GET /games/{id}", s.handleGame)
	mux.HandleFunc("POST /games/{id}/join", s.handleJoin)
	mux.HandleFunc("GET /games/{id}/events", s.handleEvents)
	mux.HandleFunc("POST /games/{id}/move", s.handleMove)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	return mux
}

func (s *Server) get(id string) *Table {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tables[id]
}

func (s *Server) handleLobby(w http.ResponseWriter, r *http.Request) {
	render(w, "lobby.html", nil)
}

// sidFromRequest returns the caller's sid cookie, minting and setting a new
// one if absent.
func sidFromRequest(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(sidCookie); err == nil && c.Value != "" {
		return c.Value
	}
	sid := randHex(16)
	http.SetCookie(w, &http.Cookie{Name: sidCookie, Value: sid, Path: "/", HttpOnly: true})
	return sid
}

// readSID reads the sid cookie without minting one - used where an absent
// cookie simply means "not seated here", not "needs a new identity".
func readSID(r *http.Request) string {
	if c, err := r.Cookie(sidCookie); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 20 {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	hard := r.FormValue("difficulty") == "hard"

	sid := sidFromRequest(w, r)
	t := NewTable(rand.New(rand.NewSource(time.Now().UnixNano())))
	t.Init(name, sid, hard)

	s.mu.Lock()
	s.tables[t.ID()] = t
	s.mu.Unlock()

	s.log.Info("table created", "id", t.ID(), "hard", hard)
	http.Redirect(w, r, "/games/"+t.ID(), http.StatusSeeOther)
}

func (s *Server) handleGame(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := s.get(id)
	if t == nil {
		http.NotFound(w, r)
		return
	}
	sid := sidFromRequest(w, r)

	if seat, ok := t.SeatForSID(sid); ok {
		render(w, "board.html", t.View(seat))
		return
	}
	if _, ok := t.FreeHumanSeat(); ok {
		render(w, "join.html", struct{ TableID string }{id})
		return
	}
	render(w, "full.html", nil)
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := s.get(id)
	if t == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	sid := sidFromRequest(w, r)
	if err := t.Join(sid, name); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	t.Broadcast()
	http.Redirect(w, r, "/games/"+id, http.StatusSeeOther)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := s.get(id)
	if t == nil {
		http.NotFound(w, r)
		return
	}
	seat, ok := t.SeatForSID(readSID(r))
	if !ok {
		http.Error(w, "not seated at this table", http.StatusForbidden)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := t.Subscribe(seat)
	defer cancel()

	writeBoardEvent(w, t.RenderFragment(seat))
	flusher.Flush()

	for {
		select {
		case frag := <-ch:
			writeBoardEvent(w, frag)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeBoardEvent(w http.ResponseWriter, data string) {
	fmt.Fprint(w, "event: board\n")
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := s.get(id)
	if t == nil {
		http.NotFound(w, r)
		return
	}
	seat, ok := t.SeatForSID(readSID(r))
	if !ok {
		http.Error(w, "not seated at this table", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	t.ApplyMove(seat, r.PostFormValue("action"), r.PostForm)
	t.Broadcast()
	w.WriteHeader(http.StatusNoContent)
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	buf.WriteTo(w)
}
