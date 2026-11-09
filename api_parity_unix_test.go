// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// Pointer assignment requires identity of the aliased public types.
var (
	_ *sock.Sockaddr           = (*uring.Sockaddr)(nil)
	_ *sock.Addr               = (*uring.Addr)(nil)
	_ *sock.RawSockaddrAny     = (*uring.RawSockaddrAny)(nil)
	_ *sock.RawSockaddr        = (*uring.RawSockaddr)(nil)
	_ *sock.RawSockaddrInet4   = (*uring.RawSockaddrInet4)(nil)
	_ *sock.RawSockaddrInet6   = (*uring.RawSockaddrInet6)(nil)
	_ *sock.RawSockaddrUnix    = (*uring.RawSockaddrUnix)(nil)
	_ *sock.NetworkType        = (*uring.NetworkType)(nil)
	_ *sock.UnderlyingProtocol = (*uring.UnderlyingProtocol)(nil)
	_ *sock.Socket             = (*uring.Socket)(nil)
	_ *iofd.PollFd             = (*uring.PollFd)(nil)
	_ *iofd.PollCloser         = (*uring.PollCloser)(nil)
	_ *zcall.Iovec             = (*uring.IoVec)(nil)
	_ *zcall.Timespec          = (*uring.Timespec)(nil)
)

// Method-expression assignment requires the exact public descriptor carriers.
var (
	_ func(*uring.Uring, uring.SQEContext, *[2]iofd.FD, uint32, ...uring.OpOptionFunc) error = (*uring.Uring).Pipe
	_ func(*uring.Uring, uint32) (iofd.FD, error)                                            = (*uring.Uring).ZCRXExport
)

// Array assignment requires equality of the aliased constant values.
var (
	_ [sock.NetworkUnix]struct{} = [uring.NetworkUnix]struct{}{}
	_ [sock.NetworkIPv4]struct{} = [uring.NetworkIPv4]struct{}{}
	_ [sock.NetworkIPv6]struct{} = [uring.NetworkIPv6]struct{}{}
)

var platformSpecificConstantValues = map[string]struct{}{
	"SizeofSockaddrAny":   {},
	"SizeofSockaddrInet4": {},
	"SizeofSockaddrInet6": {},
	"SizeofSockaddrUnix":  {},
	"AF_UNIX":             {},
	"AF_LOCAL":            {},
	"AF_INET":             {},
	"AF_INET6":            {},
	"SOCK_STREAM":         {},
	"SOCK_DGRAM":          {},
	"SOCK_RAW":            {},
	"SOCK_SEQPACKET":      {},
	"SOCK_NONBLOCK":       {},
	"SOCK_CLOEXEC":        {},
	"IPPROTO_IP":          {},
	"IPPROTO_RAW":         {},
	"IPPROTO_TCP":         {},
	"IPPROTO_UDP":         {},
	"IPPROTO_IPV6":        {},
	"IPPROTO_SCTP":        {},
	"IPPROTO_UDPLITE":     {},
	"MSG_WAITALL":         {},
	"MSG_ZEROCOPY":        {},
	"SHUT_RD":             {},
	"SHUT_WR":             {},
	"SHUT_RDWR":           {},
	"PROT_READ":           {},
	"PROT_WRITE":          {},
	"MAP_SHARED":          {},
	"MAP_POPULATE":        {},
	"O_LARGEFILE":         {},
	"AT_FDCWD":            {},
	"EPERM":               {},
	"EINTR":               {},
	"EAGAIN":              {},
	"EWOULDBLOCK":         {},
	"ENOMEM":              {},
	"EACCES":              {},
	"EFAULT":              {},
	"EBUSY":               {},
	"EEXIST":              {},
	"ENAMETOOLONG":        {},
	"ENODEV":              {},
	"EINVAL":              {},
	"EPIPE":               {},
	"EMFILE":              {},
	"ENFILE":              {},
	"ENOSYS":              {},
	"ENOTSUP":             {},
	"EINPROGRESS":         {},
	"EALREADY":            {},
	"ENOTSOCK":            {},
	"EDESTADDRREQ":        {},
	"EMSGSIZE":            {},
	"EPROTOTYPE":          {},
	"ENOPROTOOPT":         {},
	"EPROTONOSUPPORT":     {},
	"EOPNOTSUPP":          {},
	"EAFNOSUPPORT":        {},
	"EADDRINUSE":          {},
	"EADDRNOTAVAIL":       {},
	"ENETDOWN":            {},
	"ENETUNREACH":         {},
	"ENETRESET":           {},
	"ECONNABORTED":        {},
	"ECONNRESET":          {},
	"ENOBUFS":             {},
	"EISCONN":             {},
	"ENOTCONN":            {},
	"ESHUTDOWN":           {},
	"ETIMEDOUT":           {},
	"ECONNREFUSED":        {},
	"EHOSTDOWN":           {},
	"EHOSTUNREACH":        {},
	"ECANCELED":           {},
}

// Msghdr is a Linux dependency alias and a layout-compatible Darwin type.
var structurallyComparedPlatformTypes = map[string]struct{}{
	"Msghdr": {},
}

func TestPlatformAPIParity(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate package source")
	}
	dir := filepath.Dir(filename)
	if _, err := os.Stat(filename); err != nil {
		if os.IsNotExist(err) {
			t.Skip("package source unavailable in binary-only runtime")
		}
		t.Fatalf("stat package source: %v", err)
	}

	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			linux := platformAPI(t, dir, "linux", arch)
			darwin := platformAPI(t, dir, "darwin", arch)

			if difference := manifestDifference(linux, darwin); len(difference) != 0 {
				t.Errorf("Linux and Darwin typed APIs differ:\n%s", strings.Join(difference, "\n"))
			}
		})
	}
}

func platformAPI(t *testing.T, dir, goos, goarch string) map[string]string {
	t.Helper()
	ctx := build.Default
	ctx.GOOS = goos
	ctx.GOARCH = goarch
	ctx.CgoEnabled = false
	ctx.Dir = dir

	pkg, err := ctx.ImportDir(dir, build.IgnoreVendor)
	if err != nil {
		t.Fatalf("select %s/%s source: %v", goos, goarch, err)
	}

	fset := token.NewFileSet()
	var syntax []*ast.File
	sourceNames := append(append([]string(nil), pkg.GoFiles...), pkg.CgoFiles...)
	for _, name := range sourceNames {
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s for %s/%s: %v", name, goos, goarch, err)
		}
		syntax = append(syntax, file)
	}

	lookup := platformExportLookup(t, dir, goos, goarch)
	conf := types.Config{
		Importer: importer.ForCompiler(fset, runtime.Compiler, lookup),
		Sizes:    types.SizesFor(runtime.Compiler, goarch),
	}
	typed, err := conf.Check("code.hybscloud.com/uring", fset, syntax, nil)
	if err != nil {
		t.Fatalf("type-check %s/%s source: %v", goos, goarch, err)
	}
	return typedAPIManifest(typed)
}

type listedPackage struct {
	ImportPath string
	Export     string
}

func platformExportLookup(t *testing.T, dir, goos, goarch string) importer.Lookup {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-export", "-json", ".")
	cmd.Dir = dir
	cmd.Env = targetEnvironment(goos, goarch)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list %s/%s dependency exports: %v\n%s", goos, goarch, err, stderr.String())
	}

	exports := make(map[string]string)
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg listedPackage
		err := dec.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode %s/%s dependency exports: %v", goos, goarch, err)
		}
		if pkg.Export != "" {
			exports[pkg.ImportPath] = pkg.Export
		}
	}

	return func(path string) (io.ReadCloser, error) {
		export, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no %s/%s export data for %q", goos, goarch, path)
		}
		return os.Open(export)
	}
}

func targetEnvironment(goos, goarch string) []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOOS=") ||
			strings.HasPrefix(entry, "GOARCH=") ||
			strings.HasPrefix(entry, "CGO_ENABLED=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
}

func typedAPIManifest(pkg *types.Package) map[string]string {
	manifest := make(map[string]string)
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if !ast.IsExported(name) {
			continue
		}
		obj := scope.Lookup(name)
		switch obj := obj.(type) {
		case *types.Const:
			value := obj.Val().ExactString()
			if _, ok := platformSpecificConstantValues[name]; ok {
				value = "<target>"
			}
			manifest["const "+name] = canonicalType(obj.Type(), nil) + " = " + value
		case *types.Func:
			manifest["func "+name] = canonicalSignature(obj.Type().(*types.Signature), nil)
		case *types.TypeName:
			_, structural := structurallyComparedPlatformTypes[name]
			manifest["type "+name] = canonicalTypeIdentity(obj.Type(), structural)
			addMethodSet(manifest, "value", name, obj.Type())
			addMethodSet(manifest, "pointer", name, types.NewPointer(obj.Type()))
		case *types.Var:
			manifest["var "+name] = canonicalType(obj.Type(), nil)
		}
	}
	return manifest
}

func addMethodSet(manifest map[string]string, receiverKind, typeName string, typ types.Type) {
	for selection := range types.NewMethodSet(typ).Methods() {
		method := selection.Obj().(*types.Func)
		if !method.Exported() {
			continue
		}
		key := "method " + receiverKind + " " + typeName + "." + method.Name()
		manifest[key] = canonicalSignature(method.Type().(*types.Signature), nil)
	}
}

func canonicalTypeIdentity(typ types.Type, structural bool) string {
	params := make(map[*types.TypeParam]string)
	var declarationKind, name, shape string
	var list *types.TypeParamList
	switch typ := typ.(type) {
	case *types.Alias:
		declarationKind = "alias"
		name = typeNameID(typ.Obj())
		list = typ.TypeParams()
	case *types.Named:
		declarationKind = "defined"
		name = typeNameID(typ.Obj())
		list = typ.TypeParams()
	default:
		return canonicalType(typ, params)
	}
	for i := range list.Len() {
		params[list.At(i)] = fmt.Sprintf("T%d", i)
	}
	switch typ := typ.(type) {
	case *types.Alias:
		shape = canonicalType(typ.Rhs(), params)
		if structural {
			shape = canonicalPublicTypeShape(types.Unalias(typ), params)
		}
	case *types.Named:
		shape = canonicalPublicTypeShape(typ.Underlying(), params)
	}
	if structural {
		declarationKind = "structural"
	}
	return declarationKind + " " + name + canonicalTypeParameters(list, params) +
		" " + shape + " comparable=" + strconv.FormatBool(types.Comparable(typ))
}

func canonicalPublicTypeShape(typ types.Type, params map[*types.TypeParam]string) string {
	switch typ := typ.(type) {
	case *types.Alias:
		return canonicalPublicTypeShape(types.Unalias(typ), params)
	case *types.Named:
		return canonicalPublicTypeShape(typ.Underlying(), params)
	case *types.Struct:
		fields := make([]string, 0, typ.NumFields())
		for i := range typ.NumFields() {
			field := typ.Field(i)
			if !field.Exported() {
				continue
			}
			name := field.Name()
			if field.Embedded() {
				name = "embedded " + name
			}
			fields = append(fields, name+" "+canonicalType(field.Type(), params)+" "+strconv.Quote(typ.Tag(i)))
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	default:
		return canonicalType(typ, params)
	}
}

func canonicalType(typ types.Type, params map[*types.TypeParam]string) string {
	if name, ok := canonicalPlatformType(typ); ok {
		return name
	}

	switch typ := typ.(type) {
	case *types.Alias:
		return canonicalType(typ.Rhs(), params)
	case *types.Array:
		return fmt.Sprintf("[%d]%s", typ.Len(), canonicalType(typ.Elem(), params))
	case *types.Basic:
		return typ.Name()
	case *types.Chan:
		prefix := "chan "
		if typ.Dir() == types.SendOnly {
			prefix = "chan<- "
		} else if typ.Dir() == types.RecvOnly {
			prefix = "<-chan "
		}
		return prefix + canonicalType(typ.Elem(), params)
	case *types.Interface:
		typ.Complete()
		parts := make([]string, 0, typ.NumMethods()+typ.NumEmbeddeds())
		for method := range typ.Methods() {
			parts = append(parts, types.Id(method.Pkg(), method.Name())+canonicalSignature(method.Type().(*types.Signature), params))
		}
		for embedded := range typ.EmbeddedTypes() {
			parts = append(parts, canonicalType(embedded, params))
		}
		sort.Strings(parts)
		return "interface{" + strings.Join(parts, ";") + "}"
	case *types.Map:
		return "map[" + canonicalType(typ.Key(), params) + "]" + canonicalType(typ.Elem(), params)
	case *types.Named:
		return namedType(typ.Obj(), typ.TypeArgs(), params)
	case *types.Pointer:
		return "*" + canonicalType(typ.Elem(), params)
	case *types.Signature:
		return canonicalSignature(typ, params)
	case *types.Slice:
		return "[]" + canonicalType(typ.Elem(), params)
	case *types.Struct:
		fields := make([]string, typ.NumFields())
		for i := range typ.NumFields() {
			field := typ.Field(i)
			name := types.Id(field.Pkg(), field.Name())
			if field.Embedded() {
				name = "embedded " + name
			}
			fields[i] = name + " " + canonicalType(field.Type(), params) + " " + strconv.Quote(typ.Tag(i))
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	case *types.Tuple:
		return canonicalTuple(typ, false, params)
	case *types.TypeParam:
		if name, ok := params[typ]; ok {
			return name
		}
		return typ.Obj().Name()
	case *types.Union:
		terms := make([]string, typ.Len())
		for i := range typ.Len() {
			term := typ.Term(i)
			prefix := ""
			if term.Tilde() {
				prefix = "~"
			}
			terms[i] = prefix + canonicalType(term.Type(), params)
		}
		sort.Strings(terms)
		return strings.Join(terms, "|")
	default:
		return types.TypeString(typ, func(pkg *types.Package) string { return pkg.Path() })
	}
}

func canonicalPlatformType(typ types.Type) (string, bool) {
	var obj *types.TypeName
	switch typ := typ.(type) {
	case *types.Alias:
		obj = typ.Obj()
	case *types.Named:
		obj = typ.Obj()
	default:
		return "", false
	}
	if obj.Name() != "Msghdr" || obj.Pkg() == nil {
		return "", false
	}
	switch obj.Pkg().Path() {
	case "code.hybscloud.com/uring", "code.hybscloud.com/zcall":
		return "code.hybscloud.com/uring.Msghdr", true
	default:
		return "", false
	}
}

func canonicalSignature(sig *types.Signature, inherited map[*types.TypeParam]string) string {
	params := make(map[*types.TypeParam]string, len(inherited)+sig.RecvTypeParams().Len()+sig.TypeParams().Len())
	maps.Copy(params, inherited)
	for i := range sig.RecvTypeParams().Len() {
		params[sig.RecvTypeParams().At(i)] = fmt.Sprintf("R%d", i)
	}
	for i := range sig.TypeParams().Len() {
		params[sig.TypeParams().At(i)] = fmt.Sprintf("T%d", i)
	}

	return "func" + canonicalTypeParameters(sig.TypeParams(), params) +
		canonicalTuple(sig.Params(), sig.Variadic(), params) +
		canonicalTuple(sig.Results(), false, params)
}

func canonicalTypeParameters(list *types.TypeParamList, params map[*types.TypeParam]string) string {
	if list.Len() == 0 {
		return ""
	}
	items := make([]string, list.Len())
	for i := range list.Len() {
		param := list.At(i)
		items[i] = params[param] + " " + canonicalType(param.Constraint(), params)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func canonicalTuple(tuple *types.Tuple, variadic bool, params map[*types.TypeParam]string) string {
	items := make([]string, tuple.Len())
	for i := range tuple.Len() {
		typ := tuple.At(i).Type()
		if variadic && i == tuple.Len()-1 {
			items[i] = "..." + canonicalType(typ.(*types.Slice).Elem(), params)
			continue
		}
		items[i] = canonicalType(typ, params)
	}
	return "(" + strings.Join(items, ",") + ")"
}

func namedType(obj *types.TypeName, args *types.TypeList, params map[*types.TypeParam]string) string {
	name := typeNameID(obj)
	if args.Len() == 0 {
		return name
	}
	items := make([]string, args.Len())
	for i := range args.Len() {
		items[i] = canonicalType(args.At(i), params)
	}
	return name + "[" + strings.Join(items, ",") + "]"
}

func typeNameID(obj *types.TypeName) string {
	if obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func manifestDifference(left, right map[string]string) []string {
	keys := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}

	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)

	var difference []string
	for _, key := range ordered {
		leftValue, leftOK := left[key]
		rightValue, rightOK := right[key]
		switch {
		case !leftOK:
			difference = append(difference, key+" missing on Linux")
		case !rightOK:
			difference = append(difference, key+" missing on Darwin")
		case leftValue != rightValue:
			difference = append(difference, key+"\n  Linux: "+leftValue+"\n  Darwin: "+rightValue)
		}
	}
	return difference
}
