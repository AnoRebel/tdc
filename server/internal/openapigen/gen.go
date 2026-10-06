/*
Package openapigen finds, by reading the server's code, what each route's
handler binds from a request and writes in its response.

The OpenAPI document the server publishes describes bodies with the Go types
the handlers actually use — the value passed to Bind, the value passed to
JSON — rather than with shapes somebody wrote down beside them. Writing them
down by hand is the drift the server's description exists to avoid: the type a
handler binds is the body it accepts, by construction.

This runs at development time, through go generate, because it needs the
source; the server reads the result as an ordinary Go file. A test regenerates
it and fails when the committed file is behind the code.

What it cannot see it leaves out rather than guesses: a handler built by a
function call, a body read some other way, a type it cannot name from the
generated file. A route with nothing found is described without a body, and
the guide is the description for it.
*/
package openapigen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/types"
	"net/http"
	"path"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Route is one registration found in the code.
type Route struct {
	Method, Path string
	handler      *types.Func
}

// Body is what one route's handler was found to read and write.
type Body struct {
	Request types.Type
	// Query names the query parameters read one at a time.
	Query []string
	// Responses are the bodies written, by status; a nil type is a status
	// written without a body, or with one this cannot describe.
	Responses map[int][]types.Type
	// Raw marks statuses written as bytes in a content type of the handler's
	// choosing, by that content type when it is known.
	Raw map[int]string
}

var routeMethods = map[string]string{
	"GET": http.MethodGet, "POST": http.MethodPost, "PUT": http.MethodPut,
	"PATCH": http.MethodPatch, "DELETE": http.MethodDelete,
}

type analysis struct {
	pkg    *packages.Package
	decls  map[*types.Func]*ast.FuncDecl
	routes map[string]Route
	walked map[string]bool
}

// Generate loads the package in dir and returns the Go source of the file
// that records each route's bodies.
func Generate(dir string) ([]byte, error) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
		Dir: dir,
	}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		return nil, err
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		return nil, fmt.Errorf("load %s: %v", dir, loaded[0].Errors)
	}
	a := &analysis{
		pkg: loaded[0], decls: map[*types.Func]*ast.FuncDecl{},
		routes: map[string]Route{}, walked: map[string]bool{},
	}
	for _, file := range a.pkg.Syntax {
		if strings.HasSuffix(a.pkg.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				if object, ok := a.pkg.TypesInfo.Defs[fn.Name].(*types.Func); ok {
					a.decls[object] = fn
				}
			}
		}
	}
	for _, fn := range a.decls {
		a.registrations(fn, map[types.Object]string{})
	}
	return a.render()
}

/*
registrations follows one function's route registrations, knowing the prefix
of the groups passed to it.

A group is an expression whose prefix can be worked out: the Echo instance
itself, a call to Group on something whose prefix is known, or a variable or
parameter bound to one. Calls into the package that pass a group along are
followed with the parameter bound, which is how a route registered three
functions away from the group's creation gets its whole path.
*/
func (a *analysis) registrations(fn *ast.FuncDecl, groups map[types.Object]string) {
	key := fmt.Sprintf("%p%v", fn, groups)
	if a.walked[key] {
		return
	}
	a.walked[key] = true

	info := a.pkg.TypesInfo
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if i < len(node.Lhs) {
					if prefix, ok := a.prefixOf(rhs, groups); ok {
						if ident, ok := node.Lhs[i].(*ast.Ident); ok {
							if object := info.ObjectOf(ident); object != nil {
								groups[object] = prefix
							}
						}
					}
				}
			}
		case *ast.CallExpr:
			selector, ok := node.Fun.(*ast.SelectorExpr)
			if ok {
				if method, known := routeMethods[selector.Sel.Name]; known && len(node.Args) >= 2 {
					if prefix, ok := a.prefixOf(selector.X, groups); ok {
						if suffix, ok := stringValue(info, node.Args[0]); ok {
							a.addRoute(method, prefix+suffix, node.Args[1])
						}
					}
				}
			}
			a.followGroups(node, groups)
		}
		return true
	})
}

// followGroups walks into a function of this package that is handed a group.
func (a *analysis) followGroups(call *ast.CallExpr, groups map[types.Object]string) {
	callee := a.calleeOf(call.Fun)
	if callee == nil {
		return
	}
	decl := a.decls[callee]
	if decl == nil {
		return
	}
	params := parameters(a.pkg.TypesInfo, decl)
	bound := map[types.Object]string{}
	for i, arg := range call.Args {
		if i >= len(params) {
			break
		}
		if prefix, ok := a.prefixOf(arg, groups); ok {
			bound[params[i]] = prefix
		}
	}
	if len(bound) > 0 {
		a.registrations(decl, bound)
	}
}

func (a *analysis) prefixOf(expr ast.Expr, groups map[types.Object]string) (string, bool) {
	info := a.pkg.TypesInfo
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return a.prefixOf(expr.X, groups)
	case *ast.Ident:
		prefix, ok := groups[info.ObjectOf(expr)]
		return prefix, ok
	case *ast.SelectorExpr:
		if isNamed(info.TypeOf(expr), "github.com/labstack/echo/v5", "Echo") {
			return "", true
		}
	case *ast.CallExpr:
		selector, ok := expr.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Group" && len(expr.Args) >= 1 {
			if parent, ok := a.prefixOf(selector.X, groups); ok {
				if suffix, ok := stringValue(info, expr.Args[0]); ok {
					return parent + suffix, true
				}
			}
		}
	}
	return "", false
}

func (a *analysis) addRoute(method, routePath string, handler ast.Expr) {
	if !strings.HasPrefix(routePath, "/api/") {
		return
	}
	route := Route{Method: method, Path: routePath}
	if selector, ok := handler.(*ast.SelectorExpr); ok {
		if fn, ok := a.pkg.TypesInfo.Uses[selector.Sel].(*types.Func); ok {
			route.handler = fn
		}
	} else if ident, ok := handler.(*ast.Ident); ok {
		if fn, ok := a.pkg.TypesInfo.Uses[ident].(*types.Func); ok {
			route.handler = fn
		}
	}
	a.routes[method+" "+routePath] = route
}

func (a *analysis) calleeOf(fun ast.Expr) *types.Func {
	switch fun := fun.(type) {
	case *ast.Ident:
		fn, _ := a.pkg.TypesInfo.Uses[fun].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		fn, _ := a.pkg.TypesInfo.Uses[fun.Sel].(*types.Func)
		return fn
	}
	return nil
}

/*
bodies reads one handler for what it binds and writes, following it into the
package's own functions that it hands its request context to.
*/
func (a *analysis) bodies(fn *types.Func) Body {
	body := Body{Responses: map[int][]types.Type{}, Raw: map[int]string{}}
	decl := a.decls[fn]
	if decl == nil {
		return body
	}
	contexts := map[types.Object]bool{}
	for _, param := range parameters(a.pkg.TypesInfo, decl) {
		if isNamed(param.Type(), "github.com/labstack/echo/v5", "Context") {
			contexts[param] = true
		}
	}
	a.read(decl, contexts, &body, map[*ast.FuncDecl]bool{})
	return body
}

func (a *analysis) read(decl *ast.FuncDecl, contexts map[types.Object]bool, body *Body, seen map[*ast.FuncDecl]bool) {
	if seen[decl] || len(contexts) == 0 {
		return
	}
	seen[decl] = true
	info := a.pkg.TypesInfo

	isContext := func(expr ast.Expr) bool {
		ident, ok := expr.(*ast.Ident)
		return ok && contexts[info.ObjectOf(ident)]
	}

	ast.Inspect(decl.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && isContext(selector.X) {
			switch selector.Sel.Name {
			case "Bind":
				if len(call.Args) == 1 && body.Request == nil {
					if pointer, ok := info.TypeOf(call.Args[0]).(*types.Pointer); ok {
						body.Request = pointer.Elem()
					}
				}
			case "QueryParam":
				if name, ok := stringValue(info, call.Args[0]); ok && !contains(body.Query, name) {
					body.Query = append(body.Query, name)
				}
			case "JSON", "JSONPretty":
				if status, ok := intValue(info, call.Args[0]); ok && len(call.Args) >= 2 {
					body.add(status, info.TypeOf(call.Args[1]))
				}
			case "NoContent", "Redirect":
				if status, ok := intValue(info, call.Args[0]); ok {
					body.add(status, nil)
				}
			case "Blob", "Stream", "JSONBlob", "HTMLBlob":
				if status, ok := intValue(info, call.Args[0]); ok {
					contentType := "application/octet-stream"
					switch selector.Sel.Name {
					case "JSONBlob":
						contentType = "application/json"
					case "HTMLBlob":
						contentType = "text/html"
					default:
						if len(call.Args) > 1 {
							if value, ok := stringValue(info, call.Args[1]); ok {
								contentType = value
							}
						}
					}
					body.Raw[status] = contentType
				}
			}
		}

		// Into the package's own helpers that are handed the context.
		if callee := a.calleeOf(call.Fun); callee != nil {
			if next := a.decls[callee]; next != nil {
				params := parameters(info, next)
				handed := map[types.Object]bool{}
				for i, arg := range call.Args {
					if i < len(params) && isContext(arg) {
						handed[params[i]] = true
					}
				}
				a.read(next, handed, body, seen)
			}
		}
		return true
	})
}

func (b *Body) add(status int, t types.Type) {
	for _, existing := range b.Responses[status] {
		if (existing == nil && t == nil) || (existing != nil && t != nil && types.Identical(existing, t)) {
			return
		}
	}
	b.Responses[status] = append(b.Responses[status], t)
}

// render writes the generated file.
func (a *analysis) render() ([]byte, error) {
	keys := make([]string, 0, len(a.routes))
	for key := range a.routes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	imports := map[string]string{}
	self := a.pkg.Types
	qualifier := func(pkg *types.Package) string {
		if pkg == self {
			return ""
		}
		name := pkg.Name()
		for existing, path := range imports {
			if path == pkg.Path() {
				return existing
			}
			if existing == name {
				name = path2alias(pkg.Path())
			}
		}
		imports[name] = pkg.Path()
		return name
	}
	typeOf := func(t types.Type) string {
		if t == nil || !nameable(t, self, map[types.Type]bool{}) {
			return "nil"
		}
		return "reflect.TypeFor[" + types.TypeString(t, qualifier) + "]()"
	}

	var entries bytes.Buffer
	for _, key := range keys {
		route := a.routes[key]
		if route.handler == nil {
			continue
		}
		body := a.bodies(route.handler)
		if body.Request == nil && len(body.Query) == 0 && len(body.Responses) == 0 && len(body.Raw) == 0 {
			continue
		}
		fmt.Fprintf(&entries, "%q: {\n", key)
		if body.Request != nil {
			fmt.Fprintf(&entries, "Request: %s,\n", typeOf(body.Request))
		}
		if len(body.Query) > 0 {
			sort.Strings(body.Query)
			fmt.Fprintf(&entries, "Query: %#v,\n", body.Query)
		}
		statuses := make([]int, 0, len(body.Responses))
		for status := range body.Responses {
			statuses = append(statuses, status)
		}
		sort.Ints(statuses)
		if len(statuses) > 0 {
			entries.WriteString("Responses: []responseBody{\n")
			for _, status := range statuses {
				for _, t := range body.Responses[status] {
					fmt.Fprintf(&entries, "{Status: %d, Type: %s},\n", status, typeOf(t))
				}
			}
			entries.WriteString("},\n")
		}
		if len(body.Raw) > 0 {
			raw := make([]int, 0, len(body.Raw))
			for status := range body.Raw {
				raw = append(raw, status)
			}
			sort.Ints(raw)
			entries.WriteString("Raw: map[int]string{\n")
			for _, status := range raw {
				fmt.Fprintf(&entries, "%d: %q,\n", status, body.Raw[status])
			}
			entries.WriteString("},\n")
		}
		entries.WriteString("},\n")
	}

	var out bytes.Buffer
	out.WriteString("// Code generated by tdc-openapi-gen from the handlers in this package. DO NOT EDIT.\n\n")
	fmt.Fprintf(&out, "package %s\n\nimport (\n\t\"reflect\"\n", self.Name())
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if path.Base(imports[name]) == name {
			fmt.Fprintf(&out, "\t%q\n", imports[name])
		} else {
			fmt.Fprintf(&out, "\t%s %q\n", name, imports[name])
		}
	}
	out.WriteString(")\n\n// routeBodies records what each route's handler binds and writes.\nvar routeBodies = map[string]routeBodySet{\n")
	out.Write(entries.Bytes())
	out.WriteString("}\n")
	return format.Source(out.Bytes())
}

/*
nameable reports whether a type can be written in the generated file: every
named type in it is exported or belongs to the package the file is in.
*/
func nameable(t types.Type, self *types.Package, seen map[types.Type]bool) bool {
	if seen[t] {
		return true
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		object := t.Obj()
		if object.Pkg() != nil && object.Pkg() != self && !object.Exported() {
			return false
		}
		if object.Pkg() == self && object.Parent() != self.Scope() {
			// Declared inside a function: not visible to the file.
			return false
		}
		for i := range t.TypeArgs().Len() {
			if !nameable(t.TypeArgs().At(i), self, seen) {
				return false
			}
		}
		return true
	case *types.Alias:
		return nameable(types.Unalias(t), self, seen)
	case *types.Pointer:
		return nameable(t.Elem(), self, seen)
	case *types.Slice:
		return nameable(t.Elem(), self, seen)
	case *types.Array:
		return nameable(t.Elem(), self, seen)
	case *types.Map:
		return nameable(t.Key(), self, seen) && nameable(t.Elem(), self, seen)
	case *types.Struct:
		for i := range t.NumFields() {
			if !nameable(t.Field(i).Type(), self, seen) {
				return false
			}
		}
		return true
	case *types.Basic, *types.Interface:
		return true
	}
	return false
}

func parameters(info *types.Info, decl *ast.FuncDecl) []types.Object {
	var params []types.Object
	for _, field := range decl.Type.Params.List {
		for _, name := range field.Names {
			params = append(params, info.ObjectOf(name))
		}
		if len(field.Names) == 0 {
			params = append(params, nil)
		}
	}
	return params
}

func isNamed(t types.Type, pkgPath, name string) bool {
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == name && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == pkgPath
}

func stringValue(info *types.Info, expr ast.Expr) (string, bool) {
	value := info.Types[expr].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

func intValue(info *types.Info, expr ast.Expr) (int, bool) {
	value := info.Types[expr].Value
	if value == nil || value.Kind() != constant.Int {
		return 0, false
	}
	n, exact := constant.Int64Val(value)
	return int(n), exact
}

func contains(list []string, item string) bool {
	for _, each := range list {
		if each == item {
			return true
		}
	}
	return false
}

func path2alias(importPath string) string {
	parts := strings.Split(importPath, "/")
	alias := strings.Join(parts[max(0, len(parts)-2):], "")
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '.' {
			return -1
		}
		return r
	}, alias)
}
