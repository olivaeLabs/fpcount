// Package srcindex indexa os fontes Go de um módulo (somente leitura) para
// que o extrator resolva tipos, constantes e funções sem executar o código.
package srcindex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// File é um arquivo Go analisado.
type File struct {
	Rel     string // caminho relativo à raiz
	Dir     string // diretório do pacote, relativo à raiz
	AST     *ast.File
	Imports map[string]string // alias -> import path
}

// Index guarda o módulo inteiro (arquivos não-teste).
type Index struct {
	Root       string
	ModulePath string
	Fset       *token.FileSet
	Files      []*File
	byDir      map[string][]*File
	consts     map[string]map[string]string // dir -> nome -> valor string
	funcs      map[string]map[string][]*Func
	constDecls map[string][]ConstDecl
}

// ConstDecl é uma constante string declarada no pacote.
type ConstDecl struct {
	Name, Value string
	File        *File
	Line        int
}

// Func é uma função ou método declarado.
type Func struct {
	Decl *ast.FuncDecl
	File *File
}

var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, "testdata": true,
	".worktrees": true, "web": true, "docs": true, "tasks": true,
}

// Load analisa todos os .go não-teste sob root.
func Load(root string) (*Index, error) {
	ix := &Index{Root: root, Fset: token.NewFileSet(), byDir: map[string][]*File{},
		consts: map[string]map[string]string{}, funcs: map[string]map[string][]*Func{}, constDecls: map[string][]ConstDecl{}}
	if b, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "module ") {
				ix.ModulePath = strings.TrimSpace(strings.TrimPrefix(l, "module "))
			}
		}
	}
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		af, err := parser.ParseFile(ix.Fset, p, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f := &File{Rel: rel, Dir: filepath.ToSlash(filepath.Dir(rel)), AST: af, Imports: map[string]string{}}
		for _, im := range af.Imports {
			ip, _ := strconv.Unquote(im.Path.Value)
			alias := filepath.Base(ip)
			if im.Name != nil {
				alias = im.Name.Name
			}
			f.Imports[alias] = ip
		}
		ix.Files = append(ix.Files, f)
		ix.byDir[f.Dir] = append(ix.byDir[f.Dir], f)
		ix.collect(f)
	}
	return ix, nil
}

func (ix *Index) collect(f *File) {
	if ix.consts[f.Dir] == nil {
		ix.consts[f.Dir] = map[string]string{}
		ix.funcs[f.Dir] = map[string][]*Func{}
	}
	for _, d := range f.AST.Decls {
		switch dd := d.(type) {
		case *ast.GenDecl:
			if dd.Tok != token.CONST {
				continue
			}
			for _, sp := range dd.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
							v, _ := strconv.Unquote(bl.Value)
							ix.consts[f.Dir][n.Name] = v
							ix.constDecls[f.Dir] = append(ix.constDecls[f.Dir], ConstDecl{Name: n.Name, Value: v, File: f, Line: ix.Fset.Position(n.Pos()).Line})
						}
					}
				}
			}
		case *ast.FuncDecl:
			ix.funcs[f.Dir][dd.Name.Name] = append(ix.funcs[f.Dir][dd.Name.Name], &Func{Decl: dd, File: f})
		}
	}
}

// Pos formata "arquivo:linha" para evidência.
func (ix *Index) Pos(f *File, p token.Pos) string {
	return f.Rel + ":" + strconv.Itoa(ix.Fset.Position(p).Line)
}

// Dir devolve os arquivos de um diretório de pacote.
func (ix *Index) Dir(dir string) []*File { return ix.byDir[dir] }

// Dirs lista os diretórios de pacote, ordenados.
func (ix *Index) Dirs() []string {
	var out []string
	for d := range ix.byDir {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Const resolve uma constante string do pacote.
func (ix *Index) Const(dir, name string) (string, bool) {
	v, ok := ix.consts[dir][name]
	return v, ok
}

// Consts devolve as constantes string do pacote.
func (ix *Index) Consts(dir string) map[string]string { return ix.consts[dir] }

// Func devolve a primeira função/método com o nome no pacote.
func (ix *Index) Func(dir, name string) *Func {
	if fs := ix.funcs[dir][name]; len(fs) > 0 {
		return fs[0]
	}
	return nil
}

// DirOfImport converte um import path do módulo em diretório relativo.
func (ix *Index) DirOfImport(ip string) (string, bool) {
	if ix.ModulePath != "" && strings.HasPrefix(ip, ix.ModulePath+"/") {
		return strings.TrimPrefix(ip, ix.ModulePath+"/"), true
	}
	return "", false
}

// TypeDecl procura a declaração de um tipo no pacote.
func (ix *Index) TypeDecl(dir, name string) (*ast.TypeSpec, *File) {
	for _, f := range ix.byDir[dir] {
		for _, d := range f.AST.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				if ts := sp.(*ast.TypeSpec); ts.Name.Name == name {
					return ts, f
				}
			}
		}
	}
	return nil, nil
}

// ResolveFunc resolve a função chamada por `fun` (Ident local, método pelo
// nome no mesmo pacote ou pkg.Func de pacote do módulo).
func (ix *Index) ResolveFunc(f *File, fun ast.Expr) *Func {
	switch t := fun.(type) {
	case *ast.Ident:
		return ix.Func(f.Dir, t.Name)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			if ip, ok := f.Imports[id.Name]; ok {
				if dir, ok := ix.DirOfImport(ip); ok {
					return ix.Func(dir, t.Sel.Name)
				}
				return nil
			}
		}
		return ix.Func(f.Dir, t.Sel.Name) // método do mesmo pacote
	}
	return nil
}

// ConstDecls devolve as constantes string do pacote com posição.
func (ix *Index) ConstDecls(dir string) []ConstDecl { return ix.constDecls[dir] }
