// Copyright 2011-2015 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/visualfc/gotools/command"
	"github.com/visualfc/gotools/stdlib"
	"golang.org/x/tools/go/buildutil"
	"golang.org/x/tools/go/gcimporter"
	"golang.org/x/tools/go/types"
)

var Command = &command.Command{
	Run:       runTypes,
	UsageLine: "types",
	Short:     "golang type util",
	Long:      `golang type util`,
}

var (
	typesVerbose     bool
	typesAllowBinary bool
	typesFilePos     string
	typesFileStdin   bool
	typesFindUse     bool
	typesFindDef     bool
	typesFindUseAll  bool
	typesFindInfo    bool
	typesFindDoc     bool
)

//func init
func init() {
	Command.Flag.BoolVar(&typesVerbose, "v", false, "verbose debugging")
	Command.Flag.BoolVar(&typesAllowBinary, "b", false, "import can be satisfied by a compiled package object without corresponding sources.")
	Command.Flag.StringVar(&typesFilePos, "pos", "", "file position \"file.go:pos\"")
	Command.Flag.BoolVar(&typesFileStdin, "stdin", false, "input file use stdin")
	Command.Flag.BoolVar(&typesFindInfo, "info", false, "find cursor info")
	Command.Flag.BoolVar(&typesFindDef, "def", false, "find cursor define")
	Command.Flag.BoolVar(&typesFindUse, "use", false, "find cursor usages")
	Command.Flag.BoolVar(&typesFindUseAll, "all", false, "find cursor all usages in GOPATH")
	Command.Flag.BoolVar(&typesFindDoc, "doc", false, "find cursor def doc")
}

type ObjKind int

const (
	ObjNone ObjKind = iota
	ObjPkgName
	ObjTypeName
	ObjInterface
	ObjStruct
	ObjConst
	ObjVar
	ObjField
	ObjFunc
	ObjMethod
	ObjLabel
	ObjBuiltin
	ObjNil
)

var ObjKindName = []string{"none", "package",
	"type", "interface", "struct",
	"const", "var", "field",
	"func", "method",
	"label", "builtin", "nil"}

func (k ObjKind) String() string {
	if k >= 0 && int(k) < len(ObjKindName) {
		return ObjKindName[k]
	}
	return "unkwnown"
}

var builtinInfoMap = map[string]string{
	"append":  "func append(slice []Type, elems ...Type) []Type",
	"copy":    "func copy(dst, src []Type) int",
	"delete":  "func delete(m map[Type]Type1, key Type)",
	"len":     "func len(v Type) int",
	"cap":     "func cap(v Type) int",
	"make":    "func make(Type, size IntegerType) Type",
	"new":     "func new(Type) *Type",
	"complex": "func complex(r, i FloatType) ComplexType",
	"real":    "func real(c ComplexType) FloatType",
	"imag":    "func imag(c ComplexType) FloatType",
	"close":   "func close(c chan<- Type)",
	"panic":   "func panic(v interface{})",
	"recover": "func recover() interface{}",
	"print":   "func print(args ...Type)",
	"println": "func println(args ...Type)",
	"error":   "type error interface {Error() string}",
}

func builtinInfo(id string) string {
	if info, ok := builtinInfoMap[id]; ok {
		return "builtin " + info
	}
	return "builtin " + id
}

func simpleObjInfo(obj types.Object) string {
	pkg := obj.Pkg()
	s := simpleType(obj.String())
	if pkg != nil && pkg.Name() == "main" {
		return strings.Replace(s, simpleType(pkg.Path())+".", "", -1)
	}
	return s
}

func simpleType(src string) string {
	re, _ := regexp.Compile("[\\w\\./]+")
	return re.ReplaceAllStringFunc(src, func(s string) string {
		r := s
		if i := strings.LastIndex(s, "/"); i != -1 {
			r = s[i+1:]
		}
		if strings.Count(r, ".") > 1 {
			r = r[strings.Index(r, ".")+1:]
		}
		return r
	})
}

func runTypes(cmd *command.Command, args []string) error {
	if len(args) < 1 {
		cmd.Usage()
		return nil
	}
	if typesVerbose {
		now := time.Now()
		defer func() {
			log.Println("time", time.Now().Sub(now))
		}()
	}
	w := NewPkgWalker(&build.Default)
	var cursor *FileCursor
	if typesFilePos != "" {
		var cursorInfo FileCursor
		pos := strings.Index(typesFilePos, ":")
		if pos != -1 {
			cursorInfo.fileName = typesFilePos[:pos]
			if i, err := strconv.Atoi(typesFilePos[pos+1:]); err == nil {
				cursorInfo.cursorPos = i
			}
		}
		if typesFileStdin {
			src, err := ioutil.ReadAll(os.Stdin)
			if err == nil {
				cursorInfo.src = src
			}
		}
		cursor = &cursorInfo
	}
	for _, pkgName := range args {
		if pkgName == "." {
			pkgPath, err := os.Getwd()
			if err != nil {
				log.Fatalln(err)
			}
			pkgName = pkgPath
		}
		conf := &PkgConfig{IgnoreFuncBodies: true, AllowBinary: true, WithTestFiles: true}
		if cursor != nil {
			conf.Cursor = cursor
			conf.IgnoreFuncBodies = false
			conf.Info = &types.Info{
				Uses:       make(map[*ast.Ident]types.Object),
				Defs:       make(map[*ast.Ident]types.Object),
				Selections: make(map[*ast.SelectorExpr]*types.Selection),
				//Types:      make(map[ast.Expr]types.TypeAndValue),
				//Scopes : make(map[ast.Node]*types.Scope)
				//Implicits : make(map[ast.Node]types.Object)
			}
			conf.XInfo = &types.Info{
				Uses:       make(map[*ast.Ident]types.Object),
				Defs:       make(map[*ast.Ident]types.Object),
				Selections: make(map[*ast.SelectorExpr]*types.Selection),
			}
		}
		pkg, err := w.Import("", pkgName, conf)
		if pkg == nil {
			log.Fatalln("error import path", err)
		}
		if cursor != nil && (typesFindInfo || typesFindDef || typesFindUse) {
			w.LookupCursor(pkg, conf, cursor)
		}
	}
	return nil
}

type FileCursor struct {
	pkg       string
	fileName  string
	fileDir   string
	cursorPos int
	pos       token.Pos
	src       interface{}
	xtest     bool
}

type PkgConfig struct {
	IgnoreFuncBodies bool
	AllowBinary      bool
	WithTestFiles    bool
	Cursor           *FileCursor
	Pkg              *types.Package
	XPkg             *types.Package
	Info             *types.Info
	XInfo            *types.Info
	Files            map[string]*ast.File
	TestFiles        map[string]*ast.File
	XTestFiles       map[string]*ast.File
}

func NewPkgWalker(context *build.Context) *PkgWalker {
	return &PkgWalker{
		context:         context,
		fset:            token.NewFileSet(),
		parsedFileCache: map[string]*ast.File{},
		imported:        map[string]*types.Package{"unsafe": types.Unsafe},
		gcimporter:      map[string]*types.Package{"unsafe": types.Unsafe},
	}
}

type PkgWalker struct {
	fset            *token.FileSet
	context         *build.Context
	current         *types.Package
	importing       types.Package
	parsedFileCache map[string]*ast.File
	imported        map[string]*types.Package // packages already imported
	gcimporter      map[string]*types.Package
}

func contains(list []string, s string) bool {
	for _, t := range list {
		if t == s {
			return true
		}
	}
	return false
}

func (w *PkgWalker) isBinaryPkg(pkg string) bool {
	return stdlib.IsStdPkg(pkg)
}

func (w *PkgWalker) importPath(path string, mode build.ImportMode) (*build.Package, error) {
	if filepath.IsAbs(path) {
		return w.context.ImportDir(path, 0)
	}
	if stdlib.IsStdPkg(path) {
		return stdlib.ImportStdPkg(w.context, path, mode)
	}
	return w.context.Import(path, "", mode)
}

func (w *PkgWalker) Import(parentDir string, name string, conf *PkgConfig) (pkg *types.Package, err error) {
	defer func() {
		err := recover()
		if err != nil && typesVerbose {
			log.Println(err)
		}
	}()

	if strings.HasPrefix(name, ".") && parentDir != "" {
		name = filepath.Join(parentDir, name)
	}
	pkg = w.imported[name]
	if pkg != nil {
		if pkg == &w.importing {
			return nil, fmt.Errorf("cycle importing package %q", name)
		}
		return pkg, nil
	}

	if typesVerbose {
		log.Println("parser pkg", name)
	}

	bp, err := w.importPath(name, 0)
	if err != nil {
		return nil, err
	}

	checkName := name

	if bp.ImportPath == "." {
		checkName = bp.Name
	} else {
		checkName = bp.ImportPath
	}

	if err != nil {
		return nil, err
		//if _, nogo := err.(*build.NoGoError); nogo {
		//	return
		//}
		//return
		//log.Fatalf("pkg %q, dir %q: ScanDir: %v", name, info.Dir, err)
	}

	filenames := append(append([]string{}, bp.GoFiles...), bp.CgoFiles...)
	if conf.WithTestFiles {
		filenames = append(filenames, bp.TestGoFiles...)
	}

	if name == "runtime" {
		n := fmt.Sprintf("zgoos_%s.go", w.context.GOOS)
		if !contains(filenames, n) {
			filenames = append(filenames, n)
		}

		n = fmt.Sprintf("zgoarch_%s.go", w.context.GOARCH)
		if !contains(filenames, n) {
			filenames = append(filenames, n)
		}
	}

	parserFiles := func(filenames []string, cursor *FileCursor, xtest bool) (files []*ast.File) {
		for _, file := range filenames {
			var f *ast.File
			if cursor != nil && cursor.fileName == file {
				f, err = w.parseFile(bp.Dir, file, cursor.src)
				cursor.pos = token.Pos(w.fset.File(f.Pos()).Base()) + token.Pos(cursor.cursorPos)
				cursor.fileDir = bp.Dir
				cursor.xtest = xtest
			} else {
				f, err = w.parseFile(bp.Dir, file, nil)
			}
			if err != nil && typesVerbose {
				log.Printf("error parsing package %s: %s\n", name, err)
			}
			files = append(files, f)
		}
		return
	}
	files := parserFiles(filenames, conf.Cursor, false)
	xfiles := parserFiles(bp.XTestGoFiles, conf.Cursor, true)

	typesConf := types.Config{
		IgnoreFuncBodies: conf.IgnoreFuncBodies,
		FakeImportC:      true,
		Packages:         w.gcimporter,
		Import: func(imports map[string]*types.Package, name string) (pkg *types.Package, err error) {
			if pkg != nil {
				return pkg, nil
			}
			if conf.AllowBinary && w.isBinaryPkg(name) {
				pkg = w.gcimporter[name]
				if pkg != nil && pkg.Complete() {
					return
				}
				pkg, err = gcimporter.Import(imports, name)
				if pkg != nil && pkg.Complete() {
					w.gcimporter[name] = pkg
					return
				}
			}
			return w.Import(bp.Dir, name, &PkgConfig{IgnoreFuncBodies: true, AllowBinary: true, WithTestFiles: false})
		},
		Error: func(err error) {
			if typesVerbose {
				log.Println(err)
			}
		},
	}
	if pkg == nil {
		pkg, err = typesConf.Check(checkName, w.fset, files, conf.Info)
		conf.Pkg = pkg
	}
	w.imported[name] = pkg

	if len(xfiles) > 0 {
		xpkg, _ := typesConf.Check(checkName+"_test", w.fset, xfiles, conf.XInfo)
		w.imported[checkName+"_test"] = xpkg
		conf.XPkg = xpkg
	}
	return
}

func (w *PkgWalker) parseFile(dir, file string, src interface{}) (*ast.File, error) {
	filename := filepath.Join(dir, file)
	f, _ := w.parsedFileCache[filename]
	if f != nil {
		return f, nil
	}

	var err error

	// generate missing context-dependent files.
	if w.context != nil && file == fmt.Sprintf("zgoos_%s.go", w.context.GOOS) {
		src := fmt.Sprintf("package runtime; const theGoos = `%s`", w.context.GOOS)
		f, err = parser.ParseFile(w.fset, filename, src, 0)
		if err != nil {
			log.Fatalf("incorrect generated file: %s", err)
		}
	}

	if w.context != nil && file == fmt.Sprintf("zgoarch_%s.go", w.context.GOARCH) {
		src := fmt.Sprintf("package runtime; const theGoarch = `%s`", w.context.GOARCH)
		f, err = parser.ParseFile(w.fset, filename, src, 0)
		if err != nil {
			log.Fatalf("incorrect generated file: %s", err)
		}
	}

	if f == nil {
		flag := parser.AllErrors
		if typesFindDoc {
			flag |= parser.ParseComments
		}
		f, err = parser.ParseFile(w.fset, filename, src, flag)
		if err != nil {
			return f, err
		}
	}

	w.parsedFileCache[filename] = f
	return f, nil
}

func (w *PkgWalker) LookupCursor(pkg *types.Package, conf *PkgConfig, cursor *FileCursor) {
	is := w.CheckIsImport(cursor)
	if is != nil {
		if cursor.xtest {
			w.LookupImport(conf.XPkg, conf.XInfo, cursor, is)
		} else {
			w.LookupImport(conf.Pkg, conf.Info, cursor, is)
		}
	} else {
		w.LookupObjects(conf, cursor)
	}
}

func (w *PkgWalker) LookupImport(pkg *types.Package, pkgInfo *types.Info, cursor *FileCursor, is *ast.ImportSpec) {
	fpath, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return
	}

	if typesFindDef {
		fmt.Println(w.fset.Position(is.Pos()))
	}

	fbase := fpath
	pos := strings.LastIndexAny(fpath, "./-\\")
	if pos != -1 {
		fbase = fpath[pos+1:]
	}

	var fname string
	if is.Name != nil {
		fname = is.Name.Name
	} else {
		fname = fbase
	}

	if typesFindInfo {
		if fname == fpath {
			fmt.Printf("package %s\n", fname)
		} else {
			fmt.Printf("package %s (\"%s\")\n", fname, fpath)
		}
	}

	if !typesFindUse {
		return
	}

	fid := pkg.Path() + "." + fname
	var usages []int
	for id, obj := range pkgInfo.Uses {
		if obj != nil && obj.Id() == fid { //!= nil && cursorObj.Pos() == obj.Pos() {
			usages = append(usages, int(id.Pos()))
		}
	}
	(sort.IntSlice(usages)).Sort()
	for _, pos := range usages {
		fmt.Println(w.fset.Position(token.Pos(pos)))
	}
}

func parserObjKind(obj types.Object) (ObjKind, error) {
	var kind ObjKind
	switch t := obj.(type) {
	case *types.PkgName:
		kind = ObjPkgName
	case *types.Const:
		kind = ObjConst
	case *types.TypeName:
		kind = ObjTypeName
		switch t.Type().Underlying().(type) {
		case *types.Interface:
			kind = ObjInterface
		case *types.Struct:
			kind = ObjStruct
		}
	case *types.Var:
		kind = ObjVar
		if t.IsField() {
			kind = ObjField
		}
	case *types.Func:
		kind = ObjFunc
		if sig, ok := t.Type().(*types.Signature); ok {
			if sig.Recv() != nil {
				kind = ObjMethod
			}
		}
	case *types.Label:
		kind = ObjLabel
	case *types.Builtin:
		kind = ObjBuiltin
	case *types.Nil:
		kind = ObjNil
	default:
		return ObjNone, fmt.Errorf("unknown obj type %T", obj)
	}
	return kind, nil
}

func (w *PkgWalker) LookupStructFromField(info *types.Info, cursorPkg *types.Package, cursorObj types.Object, cursorPos token.Pos) types.Object {
	if info == nil {
		conf := &PkgConfig{
			IgnoreFuncBodies: true,
			AllowBinary:      true,
			WithTestFiles:    true,
			Info: &types.Info{
				Defs: make(map[*ast.Ident]types.Object),
			},
		}
		w.imported[cursorPkg.Path()] = nil
		pkg, _ := w.Import("", cursorPkg.Path(), conf)
		if pkg != nil {
			info = conf.Info
		}
	}
	for _, obj := range info.Defs {
		if obj == nil {
			continue
		}
		if _, ok := obj.(*types.TypeName); ok {
			if t, ok := obj.Type().Underlying().(*types.Struct); ok {
				for i := 0; i < t.NumFields(); i++ {
					if t.Field(i).Pos() == cursorPos {
						return obj
					}
				}
			}
		}
	}
	return nil
}

func (w *PkgWalker) lookupNamedField(named *types.Named, name string) *types.Named {
	if istruct, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < istruct.NumFields(); i++ {
			field := istruct.Field(i)
			if field.Anonymous() {
				fieldType := orgType(field.Type())
				if typ, ok := fieldType.(*types.Named); ok {
					if na := w.lookupNamedField(typ, name); na != nil {
						return na
					}
				}
			} else {
				if field.Name() == name {
					return named
				}
			}
		}
	}
	return nil
}

func (w *PkgWalker) lookupNamedMethod(named *types.Named, name string) (types.Object, *types.Named) {
	if iface, ok := named.Underlying().(*types.Interface); ok {
		for i := 0; i < iface.NumMethods(); i++ {
			fn := iface.Method(i)
			if fn.Name() == name {
				return fn, named
			}
		}
		for i := 0; i < iface.NumEmbeddeds(); i++ {
			if obj, na := w.lookupNamedMethod(iface.Embedded(i), name); obj != nil {
				return obj, na
			}
		}
		return nil, nil
	}
	if istruct, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < named.NumMethods(); i++ {
			fn := named.Method(i)
			if fn.Name() == name {
				return fn, named
			}
		}
		for i := 0; i < istruct.NumFields(); i++ {
			field := istruct.Field(i)
			if !field.Anonymous() {
				continue
			}
			if typ, ok := field.Type().(*types.Named); ok {
				if obj, na := w.lookupNamedMethod(typ, name); obj != nil {
					return obj, na
				}
			}
		}
	}
	return nil, nil
}

func IsSamePkg(a, b *types.Package) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Path() == b.Path()
}

func IsSameObject(a, b types.Object) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	var apath string
	var bpath string
	if a.Pkg() != nil {
		apath = a.Pkg().Path()
	}
	if b.Pkg() != nil {
		bpath = b.Pkg().Path()
	}
	if apath != bpath {
		return false
	}
	if a.Id() != b.Id() {
		return false
	}
	return a.String() == b.String()
}

func orgType(typ types.Type) types.Type {
	if pt, ok := typ.(*types.Pointer); ok {
		return pt.Elem()
	}
	return typ
}

func (w *PkgWalker) LookupObjects(conf *PkgConfig, cursor *FileCursor) {
	var cursorObj types.Object
	var cursorSelection *types.Selection
	var cursorObjIsDef bool
	//lookup defs

	var pkg *types.Package
	var pkgInfo *types.Info
	if cursor.xtest {
		pkgInfo = conf.XInfo
		pkg = conf.XPkg
	} else {
		pkgInfo = conf.Info
		pkg = conf.Pkg
	}

	_ = cursorObjIsDef
	if cursorObj == nil {
		for sel, obj := range pkgInfo.Selections {
			if cursor.pos >= sel.Sel.Pos() && cursor.pos <= sel.Sel.End() {
				cursorObj = obj.Obj()
				cursorSelection = obj
				break
			}
		}
	}
	if cursorObj == nil {
		for id, obj := range pkgInfo.Defs {
			if cursor.pos >= id.Pos() && cursor.pos <= id.End() {
				cursorObj = obj
				cursorObjIsDef = true
				break
			}
		}
	}
	_ = cursorSelection
	if cursorObj == nil {
		for id, obj := range pkgInfo.Uses {
			if cursor.pos >= id.Pos() && cursor.pos <= id.End() {
				cursorObj = obj
				break
			}
		}
	}
	if cursorObj == nil {
		return
	}
	kind, err := parserObjKind(cursorObj)
	if err != nil {
		log.Fatalln(err)
	}

	if kind == ObjField {
		if cursorObj.(*types.Var).Anonymous() {
			typ := orgType(cursorObj.Type())
			if named, ok := typ.(*types.Named); ok {
				cursorObj = named.Obj()
			}
		}
	}
	cursorPkg := cursorObj.Pkg()
	cursorPos := cursorObj.Pos()
	//var fieldTypeInfo *types.Info
	var fieldTypeObj types.Object
	//	if cursorPkg == pkg {
	//		fieldTypeInfo = pkgInfo
	//	}
	cursorIsInterfaceMethod := false
	var cursorInterfaceTypeName string
	if kind == ObjMethod && cursorSelection != nil && cursorSelection.Recv() != nil {
		sig := cursorObj.(*types.Func).Type().Underlying().(*types.Signature)
		if _, ok := sig.Recv().Type().Underlying().(*types.Interface); ok {
			if named, ok := cursorSelection.Recv().(*types.Named); ok {
				obj, typ := w.lookupNamedMethod(named, cursorObj.Name())
				if obj != nil {
					cursorObj = obj
				}
				if typ != nil {
					cursorPkg = typ.Obj().Pkg()
					cursorInterfaceTypeName = typ.Obj().Name()
				}
				cursorIsInterfaceMethod = true
			}
		}
	} else if kind == ObjField && cursorSelection != nil {
		if recv := cursorSelection.Recv(); recv != nil {
			typ := orgType(recv)
			if typ != nil {
				if name, ok := typ.(*types.Named); ok {
					fieldTypeObj = name.Obj()
					na := w.lookupNamedField(name, cursorObj.Name())
					if na != nil {
						fieldTypeObj = na.Obj()
					}
				}
			}
		}
	}
	if cursorPkg != nil && cursorPkg != pkg &&
		kind != ObjPkgName && w.isBinaryPkg(cursorPkg.Path()) {
		conf := &PkgConfig{
			IgnoreFuncBodies: true,
			AllowBinary:      true,
			WithTestFiles:    true,
			Info: &types.Info{
				Defs: make(map[*ast.Ident]types.Object),
			},
		}
		pkg, _ := w.Import("", cursorPkg.Path(), conf)
		if pkg != nil {
			if cursorIsInterfaceMethod {
				for _, obj := range conf.Info.Defs {
					if obj == nil {
						continue
					}
					if fn, ok := obj.(*types.Func); ok {
						if fn.Name() == cursorObj.Name() {
							if sig, ok := fn.Type().Underlying().(*types.Signature); ok {
								if named, ok := sig.Recv().Type().(*types.Named); ok {
									if named.Obj() != nil && named.Obj().Name() == cursorInterfaceTypeName {
										cursorPos = obj.Pos()
										break
									}
								}
							}
						}
					}
				}
			} else if kind == ObjField && fieldTypeObj != nil {
				for _, obj := range conf.Info.Defs {
					if obj == nil {
						continue
					}
					if _, ok := obj.(*types.TypeName); ok {
						if IsSameObject(fieldTypeObj, obj) {
							if t, ok := obj.Type().Underlying().(*types.Struct); ok {
								for i := 0; i < t.NumFields(); i++ {
									if t.Field(i).Id() == cursorObj.Id() {
										cursorPos = t.Field(i).Pos()
										break
									}
								}
							}
							break
						}
					}
				}
			} else {
				for k, v := range conf.Info.Defs {
					if k != nil && v != nil && IsSameObject(v, cursorObj) {
						cursorPos = k.Pos()
						break
					}
				}
			}
		}
		//		if kind == ObjField || cursorIsInterfaceMethod {
		//			fieldTypeInfo = conf.Info
		//		}
	}
	//	if kind == ObjField {
	//		fieldTypeObj = w.LookupStructFromField(fieldTypeInfo, cursorPkg, cursorObj, cursorPos)
	//	}
	if typesFindDef {
		fmt.Println(w.fset.Position(cursorPos))
	}
	if typesFindInfo {
		if kind == ObjField && fieldTypeObj != nil {
			typeName := fieldTypeObj.Name()
			if fieldTypeObj.Pkg() != nil && fieldTypeObj.Pkg() != pkg {
				typeName = fieldTypeObj.Pkg().Name() + "." + fieldTypeObj.Name()
			}
			fmt.Println(typeName, simpleObjInfo(cursorObj))
		} else if kind == ObjBuiltin {
			fmt.Println(builtinInfo(cursorObj.Name()))
		} else if kind == ObjPkgName {
			fmt.Println(cursorObj.String())
		} else if cursorIsInterfaceMethod {
			fmt.Println(strings.Replace(simpleObjInfo(cursorObj), "(interface)", cursorPkg.Name()+"."+cursorInterfaceTypeName, 1))
		} else {
			fmt.Println(simpleObjInfo(cursorObj))
		}
	}

	if typesFindDoc && typesFindDef {
		pos := w.fset.Position(cursorPos)
		file := w.parsedFileCache[pos.Filename]
		if file != nil {
			line := pos.Line
			var group *ast.CommentGroup
			for _, v := range file.Comments {
				lastLine := w.fset.Position(v.End()).Line
				if lastLine == line || lastLine == line-1 {
					group = v
				} else if lastLine > line {
					break
				}
			}
			if group != nil {
				fmt.Println(group.Text())
			}
		}
	}
	if !typesFindUse {
		return
	}

	var usages []int
	if kind == ObjPkgName {
		for id, obj := range pkgInfo.Uses {
			if obj != nil && obj.Id() == cursorObj.Id() { //!= nil && cursorObj.Pos() == obj.Pos() {
				usages = append(usages, int(id.Pos()))
			}
		}
	} else {
		//		for id, obj := range pkgInfo.Defs {
		//			if obj == cursorObj { //!= nil && cursorObj.Pos() == obj.Pos() {
		//				usages = append(usages, int(id.Pos()))
		//			}
		//		}
		for id, obj := range pkgInfo.Uses {
			if obj == cursorObj { //!= nil && cursorObj.Pos() == obj.Pos() {
				usages = append(usages, int(id.Pos()))
			}
		}
	}
	var pkg_path string
	var xpkg_path string
	if conf.Pkg != nil {
		pkg_path = conf.Pkg.Path()
	}
	if conf.XPkg != nil {
		xpkg_path = conf.XPkg.Path()
	}

	if cursorPkg != nil && (cursorPkg.Path() == pkg_path ||
		cursorPkg.Path() == xpkg_path) {
		usages = append(usages, int(cursorPos))
	}

	(sort.IntSlice(usages)).Sort()
	for _, pos := range usages {
		fmt.Println(w.fset.Position(token.Pos(pos)))
	}
	//check look for current pkg.object on pkg_test
	if typesFindUseAll || IsSamePkg(cursorPkg, conf.Pkg) {
		var addInfo *types.Info
		if conf.Cursor.xtest {
			addInfo = conf.Info
		} else {
			addInfo = conf.XInfo
		}
		if addInfo != nil && cursorPkg != nil {
			var usages []int
			//		for id, obj := range addInfo.Defs {
			//			if id != nil && obj != nil && obj.Id() == cursorObj.Id() {
			//				usages = append(usages, int(id.Pos()))
			//			}
			//		}
			for k, v := range addInfo.Uses {
				if k != nil && v != nil && IsSameObject(v, cursorObj) {
					usages = append(usages, int(k.Pos()))
				}
			}
			(sort.IntSlice(usages)).Sort()
			for _, pos := range usages {
				fmt.Println(w.fset.Position(token.Pos(pos)))
			}
		}
	}
	if !typesFindUseAll {
		return
	}

	if cursorPkg == nil {
		return
	}

	var find_def_pkg string
	var uses_paths []string
	if cursorPkg.Path() != pkg_path && cursorPkg.Path() != xpkg_path {
		find_def_pkg = cursorPkg.Path()
		uses_paths = append(uses_paths, cursorPkg.Path())
	}

	buildutil.ForEachPackage(&build.Default, func(importPath string, err error) {
		if err != nil {
			return
		}
		if importPath == conf.Pkg.Path() {
			return
		}
		bp, err := w.importPath(importPath, 0)
		if err != nil {
			return
		}
		find := false
		if bp.ImportPath == cursorPkg.Path() {
			find = true
		} else {
			for _, v := range bp.Imports {
				if v == cursorObj.Pkg().Path() {
					find = true
					break
				}
			}
		}
		if find {
			for _, v := range uses_paths {
				if v == bp.ImportPath {
					return
				}
			}
			uses_paths = append(uses_paths, bp.ImportPath)
		}
	})

	w.imported = make(map[string]*types.Package)
	for _, v := range uses_paths {
		conf := &PkgConfig{
			IgnoreFuncBodies: false,
			AllowBinary:      true,
			WithTestFiles:    true,
			Info: &types.Info{
				Uses: make(map[*ast.Ident]types.Object),
			},
			XInfo: &types.Info{
				Uses: make(map[*ast.Ident]types.Object),
			},
		}
		w.imported[v] = nil
		var usages []int
		vpkg, _ := w.Import("", v, conf)
		if vpkg != nil && vpkg != pkg {
			if conf.Info != nil {
				for k, v := range conf.Info.Uses {
					if k != nil && v != nil && IsSameObject(v, cursorObj) {
						usages = append(usages, int(k.Pos()))
					}
				}
			}
			if conf.XInfo != nil {
				for k, v := range conf.XInfo.Uses {
					if k != nil && v != nil && IsSameObject(v, cursorObj) {
						usages = append(usages, int(k.Pos()))
					}
				}
			}
		}
		if v == find_def_pkg {
			usages = append(usages, int(cursorPos))
		}
		(sort.IntSlice(usages)).Sort()
		for _, pos := range usages {
			fmt.Println(w.fset.Position(token.Pos(pos)))
		}
	}
}

func (w *PkgWalker) CheckIsImport(cursor *FileCursor) *ast.ImportSpec {
	if cursor.fileDir == "" {
		return nil
	}
	file, _ := w.parseFile(cursor.fileDir, cursor.fileName, cursor.src)
	if file == nil {
		return nil
	}
	for _, is := range file.Imports {
		if inRange(is, cursor.pos) {
			return is
		}
	}
	return nil
}

func inRange(node ast.Node, p token.Pos) bool {
	if node == nil {
		return false
	}
	return p >= node.Pos() && p <= node.End()
}

func (w *PkgWalker) nodeString(node interface{}) string {
	if node == nil {
		return ""
	}
	var b bytes.Buffer
	printer.Fprint(&b, w.fset, node)
	return b.String()
}
