package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/c4ptlevi/margit/engine"
	"github.com/c4ptlevi/margit/logger"
	"github.com/c4ptlevi/margit/model"
	"github.com/c4ptlevi/margit/store"
)

func newTestServer(t *testing.T, log *logger.Logger) *httptest.Server {
	t.Helper()
	eng := engine.New(store.NewMemoryStore(nil), nil, engine.Config{}, nil)
	ts := httptest.NewServer(New(eng, log))
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, ts *httptest.Server, method, path, body string, header ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func mustStatus(t *testing.T, ts *httptest.Server, method, path, body string, want int) []byte {
	t.Helper()
	resp, b := do(t, ts, method, path, body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s = %d %s, want %d", method, path, resp.StatusCode, b, want)
	}
	return b
}

func seed(t *testing.T, ts *httptest.Server) {
	t.Helper()
	mustStatus(t, ts, "PUT", "/v1/namespaces/user", `{"relations":{}}`, 204)
	mustStatus(t, ts, "PUT", "/v1/namespaces/group", `{"relations":{"member":{"types":["user"]}}}`, 204)
	mustStatus(t, ts, "PUT", "/v1/namespaces/document", `{"relations":{
		"owner":{"types":["user"]},
		"viewer_group":{"types":["group"]},
		"banned":{"types":["user"]},
		"viewer":{"expr":"(owner + viewer_group->member) - banned"}
	}}`, 204)
	mustStatus(t, ts, "POST", "/v1/tuples", `{"tuples":[
		{"object":"group:eng","relation":"member","subject":"user:bob"},
		{"object":"group:eng","relation":"member","subject":"user:dave"},
		{"object":"document:readme","relation":"owner","subject":"user:alice"},
		{"object":"document:readme","relation":"viewer_group","subject":"group:eng"},
		{"object":"document:readme","relation":"banned","subject":"user:dave"}
	]}`, 204)
}

func TestEndToEnd(t *testing.T) {
	ts := newTestServer(t, nil)
	seed(t, ts)

	for sub, want := range map[string]bool{"user:alice": true, "user:bob": true, "user:dave": false, "user:zoe": false} {
		b := mustStatus(t, ts, "POST", "/v1/check", `{"object":"document:readme","relation":"viewer","subject":"`+sub+`"}`, 200)
		var got checkResponse
		if err := json.Unmarshal(b, &got); err != nil || got.Allowed != want {
			t.Errorf("check %s = %s, want allowed=%v", sub, b, want)
		}
	}

	var exp expandResponse
	json.Unmarshal(mustStatus(t, ts, "POST", "/v1/expand", `{"object":"document:readme","relation":"viewer"}`, 200), &exp)
	if !slices.Equal(exp.Subjects, []string{"user:alice", "user:bob"}) {
		t.Errorf("expand = %v", exp.Subjects)
	}

	var lk lookupResponse
	json.Unmarshal(mustStatus(t, ts, "POST", "/v1/lookup", `{"subject":"user:bob","relation":"viewer","namespace":"document"}`, 200), &lk)
	if !slices.Equal(lk.Objects, []string{"document:readme"}) {
		t.Errorf("lookup = %v", lk.Objects)
	}

	b := mustStatus(t, ts, "POST", "/v1/lookup", `{"subject":"user:zoe","relation":"viewer","namespace":"document"}`, 200)
	if !bytes.Contains(b, []byte(`"objects":[]`)) {
		t.Errorf("empty lookup = %s, want empty array", b)
	}

	mustStatus(t, ts, "POST", "/v1/tuples/delete", `{"tuples":[{"object":"document:readme","relation":"banned","subject":"user:dave"}]}`, 204)
	b = mustStatus(t, ts, "POST", "/v1/check", `{"object":"document:readme","relation":"viewer","subject":"user:dave"}`, 200)
	if !bytes.Contains(b, []byte(`"allowed":true`)) {
		t.Errorf("check after unban = %s", b)
	}

	mustStatus(t, ts, "GET", "/healthz", "", 200)
}

func TestNamespaceEndpoints(t *testing.T) {
	ts := newTestServer(t, nil)
	seed(t, ts)

	var got namespaceResponse
	json.Unmarshal(mustStatus(t, ts, "GET", "/v1/namespaces/document", "", 200), &got)
	if got.Name != "document" || got.Relations["viewer"].Expr != "(owner + viewer_group->member) - banned" ||
		!slices.Equal(got.Relations["owner"].Types, []string{"user"}) {
		t.Fatalf("get document = %+v", got)
	}

	var list namespacesResponse
	json.Unmarshal(mustStatus(t, ts, "GET", "/v1/namespaces", "", 200), &list)
	var names []string
	for _, n := range list.Namespaces {
		names = append(names, n.Name)
	}
	if !slices.Equal(names, []string{"document", "group", "user"}) {
		t.Fatalf("list = %v", names)
	}

	mustStatus(t, ts, "GET", "/v1/namespaces/nope", "", 404)
	mustStatus(t, ts, "DELETE", "/v1/namespaces/user", "", 409)
	mustStatus(t, ts, "DELETE", "/v1/namespaces/document", "", 204)
	mustStatus(t, ts, "DELETE", "/v1/namespaces/document", "", 404)
	mustStatus(t, ts, "GET", "/v1/namespaces/document", "", 404)
	mustStatus(t, ts, "DELETE", "/v1/namespaces/group", "", 204)
	mustStatus(t, ts, "DELETE", "/v1/namespaces/user", "", 204)

	b := mustStatus(t, ts, "GET", "/v1/namespaces", "", 200)
	if !bytes.Contains(b, []byte(`"namespaces":[]`)) {
		t.Fatalf("empty list = %s, want empty array", b)
	}
}

func TestErrors(t *testing.T) {
	ts := newTestServer(t, nil)
	seed(t, ts)
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"bad json", "POST", "/v1/check", `{`, 400},
		{"unknown field", "POST", "/v1/check", `{"object":"document:readme","relation":"viewer","subject":"user:bob","x":1}`, 400},
		{"unknown namespace", "POST", "/v1/check", `{"object":"widget:1","relation":"viewer","subject":"user:bob"}`, 400},
		{"unknown relation", "POST", "/v1/check", `{"object":"document:readme","relation":"approver","subject":"user:bob"}`, 400},
		{"missing id", "POST", "/v1/check", `{"object":"document","relation":"viewer","subject":"user:bob"}`, 400},
		{"subject type", "POST", "/v1/tuples", `{"tuples":[{"object":"document:x","relation":"owner","subject":"group:eng"}]}`, 400},
		{"invalid schema", "PUT", "/v1/namespaces/project", `{"relations":{"owner":{"types":["team"]}}}`, 400},
		{"bad expression", "PUT", "/v1/namespaces/project", `{"relations":{"v":{"expr":"a +"}}}`, 400},
		{"wrong method", "GET", "/v1/check", ``, 405},
		{"no route", "GET", "/v1/nope", ``, 404},
	}
	for _, c := range cases {
		resp, b := do(t, ts, c.method, c.path, c.body)
		if resp.StatusCode != c.want {
			t.Errorf("%s: status = %d %s, want %d", c.name, resp.StatusCode, b, c.want)
			continue
		}
		if c.want == 400 {
			var eb errorBody
			if err := json.Unmarshal(b, &eb); err != nil || eb.Error == "" || eb.TraceID != resp.Header.Get(TraceHeader) {
				t.Errorf("%s: error body = %s", c.name, b)
			}
		}
	}
}

func TestStatusFor(t *testing.T) {
	cases := map[error]int{
		model.ErrNotFound:         404,
		model.ErrMaxDepthExceeded: 422,
		model.ErrNamespaceInUse:   409,
		model.ErrUnknownRelation:  400,
		context.DeadlineExceeded:  504,
		context.Canceled:          499,
		io.ErrUnexpectedEOF:       500,
	}
	for err, want := range cases {
		if got := statusFor(err); got != want {
			t.Errorf("statusFor(%v) = %d, want %d", err, got, want)
		}
	}
}

func TestTraceHeader(t *testing.T) {
	var buf bytes.Buffer
	ts := newTestServer(t, logger.New(&buf, logger.LevelInfo))

	resp, _ := do(t, ts, "GET", "/healthz", "", TraceHeader, "client-trace-1")
	first := resp.Header.Get(TraceHeader)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(first) {
		t.Fatalf("trace = %q, want server-generated id", first)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 ||
		!strings.Contains(lines[0], `pkg=api trace=`+first+` msg="request started" method=GET path=/healthz`) ||
		!strings.Contains(lines[1], `pkg=api trace=`+first+` msg="request finished" method=GET path=/healthz status=200`) {
		t.Fatalf("logs = %q", buf.String())
	}

	seen := map[string]bool{first: true}
	for range 20 {
		resp, _ = do(t, ts, "GET", "/healthz", "", TraceHeader, "client-trace-1")
		id := resp.Header.Get(TraceHeader)
		if seen[id] {
			t.Fatalf("trace id %q reused", id)
		}
		seen[id] = true
	}
}

type panicEngine struct{ engine.ReBACEngine }

func (panicEngine) Check(context.Context, model.Entity, string, model.Entity) (bool, error) {
	panic("boom")
}

func TestPanicRecovered(t *testing.T) {
	var buf bytes.Buffer
	ts := httptest.NewServer(New(panicEngine{}, logger.New(&buf, logger.LevelInfo)))
	defer ts.Close()
	resp, b := do(t, ts, "POST", "/v1/check", `{"object":"a:1","relation":"r","subject":"b:2"}`)
	if resp.StatusCode != 500 || !bytes.Contains(b, []byte("internal error")) {
		t.Fatalf("status = %d body = %s", resp.StatusCode, b)
	}
	if !strings.Contains(buf.String(), "panic in handler") {
		t.Fatalf("logs = %q", buf.String())
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(store.NewMemoryStore(nil), nil, engine.Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(eng, nil).Serve(ctx, ln, DefaultConfig()) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestDurationConfig(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"addr":":9000","read_timeout":"2s","write_timeout":"1m","shutdown_timeout":"500ms"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9000" || time.Duration(cfg.ReadTimeout) != 2*time.Second ||
		time.Duration(cfg.WriteTimeout) != time.Minute || time.Duration(cfg.ShutdownTimeout) != 500*time.Millisecond {
		t.Fatalf("cfg = %+v", cfg)
	}
	if err := json.Unmarshal([]byte(`{"read_timeout":"soon"}`), &cfg); err == nil {
		t.Fatal("bad duration accepted")
	}
}
