package api

import (
	"github.com/c4ptlevi/margit/engine"
	"github.com/c4ptlevi/margit/model"
)

type relationBody struct {
	Types []string `json:"types,omitempty"`
	Expr  string   `json:"expr,omitempty"`
}

type namespaceBody struct {
	Relations map[string]relationBody `json:"relations"`
}

func (b namespaceBody) toModel(name string) model.Namespace {
	rels := make(map[string]model.Relation, len(b.Relations))
	for relName, r := range b.Relations {
		rels[relName] = model.Relation{Name: relName, AllowedTypes: r.Types, RelExpr: r.Expr}
	}
	return model.Namespace{Name: name, Relations: rels}
}

type namespaceResponse struct {
	Name      string                  `json:"name"`
	Relations map[string]relationBody `json:"relations"`
}

type namespacesResponse struct {
	Namespaces []namespaceResponse `json:"namespaces"`
}

func namespaceFromModel(ns model.Namespace) namespaceResponse {
	rels := make(map[string]relationBody, len(ns.Relations))
	for name, r := range ns.Relations {
		rels[name] = relationBody{Types: r.AllowedTypes, Expr: r.RelExpr}
	}
	return namespaceResponse{Name: ns.Name, Relations: rels}
}

type tupleBody struct {
	Object   string `json:"object"`
	Relation string `json:"relation"`
	Subject  string `json:"subject"`
}

func (b tupleBody) toModel() model.RelationTuple {
	return model.RelationTuple{Object: parseEntity(b.Object), Relation: b.Relation, Subject: parseEntity(b.Subject)}
}

type tuplesBody struct {
	Tuples []tupleBody `json:"tuples"`
}

func (b tuplesBody) toModel() []model.RelationTuple {
	out := make([]model.RelationTuple, len(b.Tuples))
	for i, t := range b.Tuples {
		out[i] = t.toModel()
	}
	return out
}

type checkBody struct {
	tupleBody
	Consistency engine.Consistency `json:"consistency"`
}

type expandBody struct {
	Object      string             `json:"object"`
	Relation    string             `json:"relation"`
	Consistency engine.Consistency `json:"consistency"`
}

type lookupBody struct {
	Subject     string             `json:"subject"`
	Relation    string             `json:"relation"`
	Namespace   string             `json:"namespace"`
	Limit       int                `json:"limit"`
	Cursor      string             `json:"cursor"`
	Consistency engine.Consistency `json:"consistency"`
}

type checkResponse struct {
	Allowed bool `json:"allowed"`
}

type expandResponse struct {
	Subjects []string `json:"subjects"`
}

type lookupResponse struct {
	Objects    []string `json:"objects"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type errorBody struct {
	Error   string `json:"error"`
	TraceID string `json:"trace_id"`
}
