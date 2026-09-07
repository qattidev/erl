// Package erl provides method-aware HTTP routing and middleware composition.
// Configure routers and groups before serving requests. Concurrent serving is
// supported; registration concurrent with serving is not.
package erl

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Middleware wraps a handler. Returning without calling next stops the chain.
type Middleware func(next http.Handler) http.Handler

// Registrar is the registration API shared by Router and Group.
type Registrar interface {
	Use(...Middleware)
	Group(string, ...Middleware) *Group
	Handle(string, string, http.Handler, ...Middleware)
	HandleFunc(string, string, http.HandlerFunc, ...Middleware)
	GET(string, http.HandlerFunc, ...Middleware)
	POST(string, http.HandlerFunc, ...Middleware)
	PUT(string, http.HandlerFunc, ...Middleware)
	PATCH(string, http.HandlerFunc, ...Middleware)
	DELETE(string, http.HandlerFunc, ...Middleware)
	HEAD(string, http.HandlerFunc, ...Middleware)
	OPTIONS(string, http.HandlerFunc, ...Middleware)
}

// Router dispatches requests. Use New to create one.
type Router struct {
	*group
	methods  map[string]*methodTree
	get      *methodTree
	fallback http.Handler
}

// Group holds a path prefix and a snapshot of its parent's middleware.
type Group struct {
	*group
}

type group struct {
	root       *Router
	prefix     string
	middleware []Middleware
}

var (
	_ http.Handler = (*Router)(nil)
	_ Registrar    = (*Router)(nil)
	_ Registrar    = (*Group)(nil)
)

// New creates an empty router without implicit middleware.
func New() *Router {
	r := &Router{methods: make(map[string]*methodTree)}
	r.group = &group{root: r}
	r.fallback = http.HandlerFunc(r.serveFallback)
	return r
}

// Use adds middleware for subsequent registrations. On the root router, it
// also updates the chain used for generated 404, 405, and OPTIONS responses.
func (g *group) Use(middleware ...Middleware) {
	checkMiddleware(middleware)
	g.middleware = append(g.middleware, middleware...)
	if g == g.root.group {
		g.root.fallback = chain(http.HandlerFunc(g.root.serveFallback), g.middleware)
	}
}

// Group creates a subgroup. Its middleware is copied at creation time.
// Prefixes must be empty or start with '/'; trailing slashes are removed.
func (g *group) Group(prefix string, middleware ...Middleware) *Group {
	checkMiddleware(middleware)
	full := joinPath(g.prefix, prefix)
	full = strings.TrimRight(full, "/")
	parsePattern(full + "/") // Validate even when no routes are registered yet.
	m := make([]Middleware, 0, len(g.middleware)+len(middleware))
	m = append(m, g.middleware...)
	m = append(m, middleware...)
	return &Group{group: &group{root: g.root, prefix: full, middleware: m}}
}

// Handle registers a method and path. Invalid or duplicate patterns panic.
// Parameters occupy whole segments (:id); a catch-all (*path) must be last.
// Middleware runs in global, group, then route order, first wrapper outermost.
func (g *group) Handle(method, path string, handler http.Handler, middleware ...Middleware) {
	if !validMethod(method) {
		panic("erl: invalid HTTP method " + method)
	}
	if handler == nil {
		panic("erl: nil handler")
	}
	if h, ok := handler.(http.HandlerFunc); ok && h == nil {
		panic("erl: nil handler function")
	}
	checkMiddleware(middleware)
	full := joinPath(g.prefix, path)
	segments, names, staticKey, static := parsePattern(full)
	tree := g.root.methods[method]
	if tree == nil {
		tree = &methodTree{static: make(map[string]*route)}
		g.root.methods[method] = tree
		if method == http.MethodGet {
			g.root.get = tree
		}
	}
	endpoint, trail := tree.insert(segments)
	if endpoint.route != nil {
		panic("erl: duplicate route " + method + " " + full)
	}
	all := make([]Middleware, 0, len(g.middleware)+len(middleware))
	all = append(all, g.middleware...)
	all = append(all, middleware...)
	rt := &route{handler: chain(handler, all), names: names, pattern: method + " " + full}
	endpoint.route = rt
	for i := len(trail) - 1; i >= 0; i-- {
		trail[i].compress()
	}
	if static {
		tree.static[staticKey] = rt
	}
}

// HandleFunc registers a standard Go handler function.
func (g *group) HandleFunc(method, path string, handler http.HandlerFunc, middleware ...Middleware) {
	g.Handle(method, path, handler, middleware...)
}

// GET registers a GET route, also used for HEAD unless a HEAD route matches.
func (g *group) GET(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodGet, path, h, m...)
}

// POST registers a POST route.
func (g *group) POST(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodPost, path, h, m...)
}

// PUT registers a PUT route.
func (g *group) PUT(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodPut, path, h, m...)
}

// PATCH registers a PATCH route.
func (g *group) PATCH(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodPatch, path, h, m...)
}

// DELETE registers a DELETE route.
func (g *group) DELETE(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodDelete, path, h, m...)
}

// HEAD registers an explicit HEAD route.
func (g *group) HEAD(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodHead, path, h, m...)
}

// OPTIONS registers an explicit OPTIONS route.
func (g *group) OPTIONS(path string, h http.HandlerFunc, m ...Middleware) {
	g.Handle(http.MethodOptions, path, h, m...)
}

// ServeHTTP selects a route and populates PathValue and Pattern before invoking
// middleware. Paths are case-sensitive and are neither cleaned nor redirected.
// The original ResponseWriter is passed through unchanged.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path, escaped := requestPath(req)
	tree := r.get
	if req.Method != http.MethodGet {
		tree = r.methods[req.Method]
	}
	var scratch [8]string
	rt, values := tree.find(path, escaped, scratch[:0])
	if rt == nil && req.Method == http.MethodHead {
		rt, values = r.get.find(path, escaped, scratch[:0])
	}
	if rt == nil {
		req.Pattern = ""
		r.fallback.ServeHTTP(w, req)
		return
	}
	req.Pattern = rt.pattern
	for i, name := range rt.names {
		req.SetPathValue(name, values[i])
	}
	rt.handler.ServeHTTP(w, req)
}

func (r *Router) serveFallback(w http.ResponseWriter, req *http.Request) {
	path, escaped := requestPath(req)
	var scratch [8]string
	var allowed []string
	hasHEAD, hasGET, hasOPTIONS := false, false, false
	for method, tree := range r.methods {
		if rt, _ := tree.find(path, escaped, scratch[:0]); rt != nil {
			allowed = append(allowed, method)
			hasHEAD = hasHEAD || method == http.MethodHead
			hasGET = hasGET || method == http.MethodGet
			hasOPTIONS = hasOPTIONS || method == http.MethodOptions
		}
	}
	if len(allowed) == 0 {
		http.NotFound(w, req)
		return
	}
	if hasGET && !hasHEAD {
		allowed = append(allowed, http.MethodHead)
	}
	if !hasOPTIONS {
		allowed = append(allowed, http.MethodOptions)
	}
	sort.Strings(allowed)
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func requestPath(req *http.Request) (string, bool) {
	if req.URL == nil {
		return "", false
	}
	if req.URL.RawPath != "" {
		return req.URL.EscapedPath(), true
	}
	return req.URL.Path, false
}

func chain(h http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
		if h == nil {
			panic("erl: middleware returned nil handler")
		}
	}
	return h
}

func checkMiddleware(middleware []Middleware) {
	for _, m := range middleware {
		if m == nil {
			panic("erl: nil middleware")
		}
	}
}

func joinPath(prefix, path string) string {
	if path != "" && path[0] != '/' {
		panic("erl: paths must start with '/'")
	}
	if prefix+path == "" {
		return "/"
	}
	return prefix + path
}

func validMethod(method string) bool {
	if method == "" {
		return false
	}
	for i := 0; i < len(method); i++ {
		c := method[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func decodeSegment(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s // EscapedPath produces valid escaping for real requests.
	}
	return decoded
}
