package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	placeholder = "0000"
	prefix      = "tag_"
	tagLen      = 6
	alphabet    = "abcdefghijklmnopqrstuvwxyz0123456789"
)

var tagPattern = regexp.MustCompile(`^(?:tag_)?([a-z0-9]{6})$`)

type site struct {
	file       string
	start, end int
	tag        string
	pos        token.Position
}

func (s site) id() string {
	if m := tagPattern.FindStringSubmatch(s.tag); m != nil {
		return m[1]
	}
	return ""
}

func main() {
	check := flag.Bool("check", false, "report missing or duplicate tags without rewriting")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	n, err := run(root, *check, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *check && n > 0 {
		os.Exit(1)
	}
}

func run(root string, check bool, out io.Writer) (int, error) {
	sites, err := collect(root)
	if err != nil {
		return 0, err
	}

	used := map[string]bool{}
	var todo []site
	for _, s := range sites {
		if id := s.id(); id != "" && !used[id] {
			used[id] = true
			if s.tag == prefix+id {
				continue
			}
		} else {
			s.tag = ""
		}
		todo = append(todo, s)
	}

	if check {
		for _, s := range todo {
			reason := "missing " + prefix + " prefix in"
			if s.tag == "" {
				reason = "unfilled, invalid or duplicate"
			}
			fmt.Fprintf(out, "%s: %s log tag\n", s.pos, reason)
		}
		return len(todo), nil
	}

	byFile := map[string][]site{}
	for _, s := range todo {
		id := s.id()
		if id == "" {
			id = newTag(used)
		}
		s.tag = prefix + id
		byFile[s.file] = append(byFile[s.file], s)
		fmt.Fprintf(out, "%s: tag %s\n", s.pos, s.tag)
	}
	for file, edits := range byFile {
		if err := rewrite(file, edits); err != nil {
			return 0, err
		}
	}
	return len(todo), nil
}

type parsedFile struct {
	path string
	file *ast.File
}

func collect(root string) ([]site, error) {
	fset := token.NewFileSet()
	var files []parsedFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files = append(files, parsedFile{path, f})
		return nil
	})
	if err != nil {
		return nil, err
	}

	taggers := map[string]bool{}
	for _, pf := range files {
		for _, decl := range pf.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && secondParamIsTag(fn.Type) {
				taggers[fn.Name.Name] = true
			}
		}
	}

	var sites []site
	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 || !taggers[calleeName(call.Fun)] {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			tag, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			sites = append(sites, site{
				file:  pf.path,
				start: fset.Position(lit.Pos()).Offset,
				end:   fset.Position(lit.End()).Offset,
				tag:   tag,
				pos:   fset.Position(lit.Pos()),
			})
			return true
		})
	}
	return sites, nil
}

func secondParamIsTag(ft *ast.FuncType) bool {
	var names []string
	for _, field := range ft.Params.List {
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
	}
	return len(names) >= 2 && names[1] == "tag"
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func rewrite(file string, edits []site) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	slices.SortFunc(edits, func(a, b site) int { return b.start - a.start })
	for _, e := range edits {
		src = slices.Concat(src[:e.start], []byte(strconv.Quote(e.tag)), src[e.end:])
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, src, info.Mode())
}

func newTag(used map[string]bool) string {
	b := make([]byte, tagLen)
	for {
		rand.Read(b)
		for i := range b {
			b[i] = alphabet[int(b[i])%len(alphabet)]
		}
		if tag := string(b); !used[tag] {
			used[tag] = true
			return tag
		}
	}
}
