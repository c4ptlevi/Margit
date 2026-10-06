package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/c4ptlevi/margit/cache"
	"github.com/c4ptlevi/margit/engine"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
)

const (
	TraceHeader   = "X-Trace-Id"
	maxBodyBytes  = 1 << 20
	slowThreshold = time.Second
)

type Server struct {
	engine  engine.ReBACEngine
	log     *logger.Logger
	mux     *http.ServeMux
	metrics *metrics
}

func New(eng engine.ReBACEngine, log *logger.Logger) *Server {
	s := &Server{engine: eng, log: log, mux: http.NewServeMux(), metrics: newMetrics()}
	if c, ok := eng.(engine.CacheStatser); ok {
		s.RegisterCache(context.Background(), "response", c)
	}
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /v1/namespaces", s.listNamespaces)
	s.mux.HandleFunc("GET /v1/namespaces/{name}", s.getNamespace)
	s.mux.HandleFunc("PUT /v1/namespaces/{name}", s.saveNamespace)
	s.mux.HandleFunc("DELETE /v1/namespaces/{name}", s.deleteNamespace)
	s.mux.HandleFunc("POST /v1/tuples", s.writeTuples)
	s.mux.HandleFunc("POST /v1/tuples/delete", s.deleteTuples)
	s.mux.HandleFunc("POST /v1/check", s.check)
	s.mux.HandleFunc("POST /v1/expand", s.expand)
	s.mux.HandleFunc("POST /v1/lookup", s.lookup)
	return s
}

func (s *Server) RegisterCache(ctx context.Context, name string, c cache.Statser) {
	s.metrics.registerCache(name, c)
	s.log.Info(ctx, "tag_3yn5aw", "cache metrics registered", "cache", name)
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
	if r.URL.Path == MetricsPath {
		s.metrics.handler.ServeHTTP(w, r)
		return
	}
	start := time.Now()
	s.metrics.inFlight.Inc()
	id := logger.NewTraceID()
	ctx := logger.WithTraceID(r.Context(), id)
	r = r.WithContext(ctx)
	w.Header().Set(TraceHeader, id)
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.log.Info(ctx, "tag_9dg7na", "request started", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr,
		"bytes", r.ContentLength, "agent", r.UserAgent())

	defer func() {
		if p := recover(); p != nil {
			s.log.Error(ctx, "tag_wasvgo", "panic in handler", "panic", p, "stack", string(debug.Stack()))
			if !rec.wrote {
				s.writeJSON(rec, r, http.StatusInternalServerError, errorBody{Error: "internal error", TraceID: id})
			}
		}
		took := time.Since(start)
		s.metrics.inFlight.Dec()
		s.metrics.observe(r, rec.status, took)
		s.log.Info(ctx, "tag_irdd58", "request finished", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", took)
		if took >= slowThreshold {
			s.log.Warn(ctx, "tag_6cy0aj", "slow request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", took, "threshold", slowThreshold)
		}
	}()
	s.mux.ServeHTTP(rec, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) saveNamespace(w http.ResponseWriter, r *http.Request) {
	var body namespaceBody
	if !s.decode(w, r, &body) {
		return
	}
	s.log.Debug(r.Context(), "tag_84hh1s", "save namespace", "namespace", r.PathValue("name"), "relations", len(body.Relations))
	if err := s.engine.SaveNamespace(r.Context(), body.toModel(r.PathValue("name"))); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getNamespace(w http.ResponseWriter, r *http.Request) {
	s.log.Debug(r.Context(), "tag_efu0n9", "get namespace", "namespace", r.PathValue("name"))
	ns, err := s.engine.GetNamespace(r.Context(), r.PathValue("name"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, namespaceFromModel(ns))
}

func (s *Server) listNamespaces(w http.ResponseWriter, r *http.Request) {
	all, err := s.engine.ListNamespaces(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := namespacesResponse{Namespaces: make([]namespaceResponse, len(all))}
	for i, ns := range all {
		out.Namespaces[i] = namespaceFromModel(ns)
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) deleteNamespace(w http.ResponseWriter, r *http.Request) {
	s.log.Debug(r.Context(), "tag_rlkr6s", "delete namespace", "namespace", r.PathValue("name"))
	if err := s.engine.DeleteNamespace(r.Context(), r.PathValue("name")); err != nil {
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
	s.log.Debug(r.Context(), "tag_nvcz6x", "write tuples", "count", len(body.Tuples))
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
	s.log.Debug(r.Context(), "tag_y9la7q", "delete tuples", "count", len(body.Tuples))
	if err := s.engine.DeleteTuples(r.Context(), body.toModel()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var body checkBody
	if !s.decode(w, r, &body) {
		return
	}
	ctx, ok := s.consistency(w, r, body.Consistency)
	if !ok {
		return
	}
	t := body.toModel()
	s.log.Debug(ctx, "tag_7xpdmo", "check", "object", t.Object, "relation", t.Relation, "subject", t.Subject, "consistency", body.Consistency)
	allowed, err := s.engine.Check(ctx, t.Object, t.Relation, t.Subject)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.metrics.checkResult(allowed)
	s.writeJSON(w, r, http.StatusOK, checkResponse{Allowed: allowed})
}

func (s *Server) expand(w http.ResponseWriter, r *http.Request) {
	var body expandBody
	if !s.decode(w, r, &body) {
		return
	}
	ctx, ok := s.consistency(w, r, body.Consistency)
	if !ok {
		return
	}
	s.log.Debug(ctx, "tag_kx992n", "expand", "object", body.Object, "relation", body.Relation, "consistency", body.Consistency)
	subjects, err := s.engine.Expand(ctx, parseEntity(body.Object), body.Relation)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, expandResponse{Subjects: entityStrings(subjects)})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	var body lookupBody
	if !s.decode(w, r, &body) {
		return
	}
	ctx, ok := s.consistency(w, r, body.Consistency)
	if !ok {
		return
	}
	s.log.Debug(ctx, "tag_lv8kcg", "lookup", "subject", body.Subject, "relation", body.Relation, "namespace", body.Namespace,
		"cursor", body.Cursor, "limit", body.Limit, "consistency", body.Consistency)
	objects, next, err := s.engine.Lookup(ctx, parseEntity(body.Subject), body.Relation, body.Namespace,
		engine.Page{After: body.Cursor, Limit: body.Limit})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, lookupResponse{Objects: entityStrings(objects), NextCursor: next})
}

func (s *Server) consistency(w http.ResponseWriter, r *http.Request, c engine.Consistency) (context.Context, bool) {
	switch c {
	case "", engine.MinimizeLatency, engine.FullyConsistent:
		return engine.WithConsistency(r.Context(), c), true
	}
	s.log.Debug(r.Context(), "tag_05x39z", "invalid consistency", "consistency", c)
	s.fail(w, r, badRequestError{fmt.Errorf("consistency must be %q or %q", engine.MinimizeLatency, engine.FullyConsistent)})
	return nil, false
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
		case model.ErrNamespaceInUse:
			return http.StatusConflict
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
		s.log.Error(ctx, "tag_ddx87w", "request failed", "method", r.Method, "path", r.URL.Path, "status", status, "err", err)
		if status == http.StatusInternalServerError {
			msg = "internal error"
		}
	} else {
		s.log.Debug(ctx, "tag_tfnzrn", "request rejected", "method", r.Method, "path", r.URL.Path, "status", status, "err", err)
	}
	s.writeJSON(w, r, status, errorBody{Error: msg, TraceID: logger.TraceID(ctx)})
}

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Warn(r.Context(), "tag_y0diih", "response write failed", "method", r.Method, "path", r.URL.Path, "status", status, "err", err)
	}
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
