package srcindex

import (
	"go/ast"
	"reflect"
	"sort"
	"strings"
)

const maxDepth = 6

// Resolve devolve a declaração nomeada (Ident local ou pkg.Tipo do módulo).
func (ix *Index) Resolve(f *File, e ast.Expr) (*ast.TypeSpec, *File) {
	switch t := e.(type) {
	case *ast.Ident:
		return ix.TypeDecl(f.Dir, t.Name)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			if ip, ok := f.Imports[id.Name]; ok {
				if dir, ok := ix.DirOfImport(ip); ok {
					return ix.TypeDecl(dir, t.Sel.Name)
				}
			}
		}
	case *ast.StarExpr:
		return ix.Resolve(f, t.X)
	case *ast.ParenExpr:
		return ix.Resolve(f, t.X)
	}
	return nil, nil
}

// LeafFields devolve os campos-folha (DET candidatos) de um tipo, achatando
// structs aninhadas do módulo, e o número de tipos-struct distintos envolvidos
// (raiz + subgrupos: base do RET de funções de dados).
func (ix *Index) LeafFields(f *File, e ast.Expr) ([]string, int) {
	leaves, keys := ix.LeafFieldsKeys(f, e)
	return leaves, len(keys)
}

// LeafFieldsKeys é como LeafFields, mas devolve as chaves dos tipos-struct
// (arquivo#Tipo) para permitir união entre vários documentos.
func (ix *Index) LeafFieldsKeys(f *File, e ast.Expr) ([]string, []string) {
	groups := map[string]bool{}
	var leaves []string
	ix.walk(f, e, "", map[string]bool{}, groups, &leaves, 0)
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return leaves, keys
}

func (ix *Index) walk(f *File, e ast.Expr, prefix string, path, groups map[string]bool, out *[]string, depth int) {
	leaf := func() {
		if prefix != "" {
			*out = append(*out, prefix)
		}
	}
	if depth > maxDepth {
		leaf()
		return
	}
	switch t := e.(type) {
	case *ast.StarExpr:
		ix.walk(f, t.X, prefix, path, groups, out, depth)
		return
	case *ast.ParenExpr:
		ix.walk(f, t.X, prefix, path, groups, out, depth)
		return
	case *ast.ArrayType:
		ix.walk(f, t.Elt, prefix, path, groups, out, depth)
		return
	case *ast.Ellipsis:
		ix.walk(f, t.Elt, prefix, path, groups, out, depth)
		return
	case *ast.StructType:
		ix.walkStruct(f, t, "<anon:"+prefix+">", prefix, path, groups, out, depth)
		return
	case *ast.Ident, *ast.SelectorExpr:
		if ts, tf := ix.Resolve(f, t); ts != nil {
			key := tf.Rel + "#" + ts.Name.Name
			if st, ok := ts.Type.(*ast.StructType); ok {
				if path[key] {
					leaf()
					return
				}
				path[key] = true
				ix.walkStruct(tf, st, key, prefix, path, groups, out, depth)
				delete(path, key)
				return
			}
			ix.walk(tf, ts.Type, prefix, path, groups, out, depth+1)
			return
		}
	}
	leaf()
}

func (ix *Index) walkStruct(f *File, st *ast.StructType, key, prefix string, path, groups map[string]bool, out *[]string, depth int) {
	groups[key] = true
	for _, fld := range st.Fields.List {
		tag := ""
		if fld.Tag != nil {
			tag = strings.Trim(fld.Tag.Value, "`")
		}
		st := reflect.StructTag(tag)
		name, skip := tagName(st, "json")
		if bn, bskip := tagName(st, "bson"); bn != "" || bskip {
			if name == "" {
				name, skip = bn, bskip
			}
		}
		if skip {
			continue
		}
		if len(fld.Names) == 0 { // embutido
			ix.walk(f, fld.Type, prefix, path, groups, out, depth+1)
			continue
		}
		for _, n := range fld.Names {
			fname := name
			if fname == "" {
				if !n.IsExported() {
					continue
				}
				fname = n.Name
			}
			p := fname
			if prefix != "" {
				p = prefix + "." + fname
			}
			ix.walk(f, fld.Type, p, path, groups, out, depth+1)
		}
	}
}

func tagName(t reflect.StructTag, key string) (name string, skip bool) {
	v, ok := t.Lookup(key)
	if !ok {
		return "", false
	}
	n := strings.Split(v, ",")[0]
	if n == "-" {
		return "", true
	}
	return n, false
}
