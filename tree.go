package erl

import (
	"net/http"
	"net/url"
	"strings"
)

type route struct {
	handler http.Handler
	names   []string
	pattern string
}

// Literal edges contain a complete path segment, rather than one byte per node.
type node struct {
	literal  map[string]*node
	param    *node
	catchAll *node
	route    *route
	prefix   string // Collapsed consecutive literal edges, for unescaped paths.
	next     *node
}

type methodTree struct {
	root   node
	static map[string]*route
}

type segment struct {
	kind byte
	text string
}

func parsePattern(path string) (segments []segment, names []string, staticKey string, static bool) {
	if path == "" || path[0] != '/' || strings.ContainsAny(path, "?#\r\n\x00") {
		panic("erl: invalid route path " + path)
	}
	parts := strings.Split(path[1:], "/")
	static = true
	decoded := make([]string, len(parts))
	seen := make(map[string]bool)
	for i, part := range parts {
		if len(part) > 0 && (part[0] == ':' || part[0] == '*') {
			name := part[1:]
			if !validName(name) || seen[name] || (part[0] == '*' && i != len(parts)-1) {
				panic("erl: invalid wildcard in " + path)
			}
			seen[name] = true
			names = append(names, name)
			segments = append(segments, segment{kind: part[0]})
			static = false
		} else {
			if strings.ContainsAny(part, ":*") {
				panic("erl: wildcards must occupy whole segments in " + path)
			}
			literal, err := url.PathUnescape(part)
			if err != nil {
				panic("erl: invalid URL escape in " + path)
			}
			if strings.Contains(literal, "/") {
				static = false // This literal must be matched as a single segment.
			}
			decoded[i] = literal
			segments = append(segments, segment{text: literal})
		}
	}
	if static {
		staticKey = "/" + strings.Join(decoded, "/")
	}
	return
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

func (t *methodTree) insert(segments []segment) (*node, []*node) {
	n := &t.root
	trail := []*node{n}
	for _, s := range segments {
		switch s.kind {
		case ':':
			if n.param == nil {
				n.param = &node{}
			}
			n = n.param
		case '*':
			if n.catchAll == nil {
				n.catchAll = &node{}
			}
			n = n.catchAll
		default:
			if n.literal == nil {
				n.literal = make(map[string]*node)
			}
			child := n.literal[s.text]
			if child == nil {
				child = &node{}
				n.literal[s.text] = child
			}
			n = child
		}
		trail = append(trail, n)
	}
	return n, trail
}

// compress updates only nodes along the insertion path. Keeping the original
// segment edges allows escaped requests to take the segment-aware slow path.
func (n *node) compress() {
	n.prefix, n.next = "", nil
	if n.route != nil || n.param != nil || n.catchAll != nil || len(n.literal) != 1 {
		return
	}
	for label, child := range n.literal {
		if strings.Contains(label, "/") {
			return
		}
		n.prefix, n.next = "/"+label, child
		if child.next != nil {
			n.prefix += child.prefix
			n.next = child.next
		}
	}
}

func (t *methodTree) find(path string, escaped bool, values []string) (*route, []string) {
	if t == nil || path == "" || path[0] != '/' {
		return nil, values
	}
	if !escaped {
		if rt := t.static[path]; rt != nil {
			return rt, values
		}
	}
	return t.root.match(path, escaped, values)
}

func (n *node) match(path string, escaped bool, values []string) (*route, []string) {
	if !escaped && n.next != nil {
		if !strings.HasPrefix(path, n.prefix) {
			return nil, values
		}
		path = path[len(n.prefix):]
		if path != "" && path[0] != '/' {
			return nil, values
		}
		n = n.next
	}
	if path == "" {
		return n.route, values
	}
	part, rest := path[1:], ""
	if slash := strings.IndexByte(part, '/'); slash >= 0 {
		part, rest = part[:slash], part[slash:]
	}
	if escaped {
		part = decodeSegment(part)
	}
	if child := n.literal[part]; child != nil {
		if rt, found := child.match(rest, escaped, values); rt != nil {
			return rt, found
		}
	}
	if n.param != nil && part != "" {
		if rt, found := n.param.match(rest, escaped, append(values, part)); rt != nil {
			return rt, found
		}
	}
	if n.catchAll != nil {
		tail := path[1:]
		if escaped {
			tail = decodeSegment(tail)
		}
		return n.catchAll.route, append(values, tail)
	}
	return nil, values
}
