package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/c4ptlevi/margit/engine"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

const (
	TraceHeader  = "X-Trace-Id"
	maxBodyBytes = 1 << 20
)

type Server struct {
	engine engine.ReBACEngine
	log    *logger.Logger
	mux    *http.ServeMux
}

func New(eng engine.ReBACEngine, log *logger.Logger) *Server {
	s := &Server{engine: eng, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("PUT /v1/namespaces/{name}", s.saveNamespace)
	s.mux.HandleFunc("POST /v1/tuples", s.writeTuples)
	s.mux.HandleFunc("POST /v1/tuples/delete", s.deleteTuples)
	s.mux.HandleFunc("POST /v1/check", s.check)
	s.mux.HandleFunc("POST /v1/expand", s.expand)
	s.mux.HandleFunc("POST /v1/lookup", s.lookup)
	return s
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.status, r.wrote = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status, r.wrote = http.StatusOK, true
	}
	return r.ResponseWriter.Write(b)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := logger.NewTraceID()
	ctx := logger.WithTraceID(r.Context(), id)
	r = r.WithContext(ctx)
	w.Header().Set(TraceHeader, id)
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.log.Info(ctx, "tag_9dg7na", "request started", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)

	defer func() {
		if p := recover(); p != nil {
			s.log.Error(ctx, "tag_wasvgo", "panic in handler", "panic", p, "stack", string(debug.Stack()))
			if !rec.wrote {
				writeJSON(rec, http.StatusInternalServerError, errorBody{Error: "internal error", TraceID: id})
			}
		}
		s.log.Info(ctx, "tag_irdd58", "request finished", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", time.Since(start))
	}()
	s.mux.ServeHTTP(rec, r)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) saveNamespace(w http.ResponseWriter, r *http.Request) {
	var body namespaceBody
	if !s.decode(w, r, &body) {
		return
	}
	if err := s.engine.SaveNamespace(r.Context(), body.toModel(r.PathValue("name"))); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeTuples(w http.ResponseWriter, r *http.Request) {
	var body tuplesBody
	if !s.decode(w, r, &body) {
		return
	}
	if err := s.engine.WriteTuples(r.Context(), body.toModel()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteTuples(w http.ResponseWriter, r *http.Request) {
	var body tuplesBody
	if !s.decode(w, r, &body) {
		return
	}
	if err := s.engine.DeleteTuples(r.Context(), body.toModel()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var body tupleBody
	if !s.decode(w, r, &body) {
		return
	}
	t := body.toModel()
	ok, err := s.engine.Check(r.Context(), t.Object, t.Relation, t.Subject)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkResponse{Allowed: ok})
}

func (s *Server) expand(w http.ResponseWriter, r *http.Request) {
	var body expandBody
	if !s.decode(w, r, &body) {
		return
	}
	subjects, err := s.engine.Expand(r.Context(), parseEntity(body.Object), body.Relation)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, expandResponse{Subjects: entityStrings(subjects)})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	var body lookupBody
	if !s.decode(w, r, &body) {
		return
	}
	objects, err := s.engine.Lookup(r.Context(), parseEntity(body.Subject), body.Relation, body.Namespace)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, lookupResponse{Objects: entityStrings(objects)})
}

type badRequestError struct{ err error }

func (e badRequestError) Error() string { return "invalid request body: " + e.err.Error() }

func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		s.fail(w, r, badRequestError{err})
		return false
	}
	return true
}

func statusFor(err error) int {
	var me model.Error
	var br badRequestError
	switch {
	case errors.As(err, &br):
		return http.StatusBadRequest
	case errors.As(err, &me):
		switch me {
		case model.ErrNotFound:
			return http.StatusNotFound
		case model.ErrMaxDepthExceeded:
			return http.StatusUnprocessableEntity
		}
		return http.StatusBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		return 499
	}
	return http.StatusInternalServerError
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	status := statusFor(err)
	msg := err.Error()
	if status >= http.StatusInternalServerError {
		s.log.Error(ctx, "tag_ddx87w", "request failed", "status", status, "err", err)
		if status == http.StatusInternalServerError {
			msg = "internal error"
		}
	} else {
		s.log.Debug(ctx, "tag_tfnzrn", "request rejected", "status", status, "err", err)
	}
	writeJSON(w, status, errorBody{Error: msg, TraceID: logger.TraceID(ctx)})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func parseEntity(s string) model.Entity {
	ns, id, _ := strings.Cut(s, ":")
	return model.Entity{Namespace: ns, ID: id}
}

func entityStrings(es []model.Entity) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.String()
	}
	return out
}
