// Package lint checks agent instruction files (AGENTS.md, CLAUDE.md,
// copilot-instructions.md and friends) against the repository they describe.
package lint

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Repo is a read-only view of a repository: its file list and file contents.
type Repo interface {
	Files() []string // slash-separated paths relative to the root, sorted
	Read(p string) ([]byte, error)
	IsLink(p string) (target string, ok bool)
}

// Dir is a repository checked out on disk.
type Dir struct {
	Root  string
	files []string
	links map[string]string
}

// Only directories that are never meaningful to instructions; committed
// build/, dist/ or vendor/ trees are real paths and must stay visible.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "__pycache__": true}

// OpenDir walks root, skipping dependency and build directories.
func OpenDir(root string) (*Dir, error) {
	d := &Dir{Root: root, links: map[string]string{}}
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			if rel != "." && skipDirs[e.Name()] {
				return filepath.SkipDir
			}
			if rel != "." {
				d.files = append(d.files, rel+"/")
			}
			return nil
		}
		if e.Type()&fs.ModeSymlink != 0 {
			if t, err := os.Readlink(p); err == nil {
				d.links[rel] = t
			}
		}
		d.files = append(d.files, rel)
		return nil
	})
	sort.Strings(d.files)
	return d, err
}

func (d *Dir) Files() []string { return d.files }
func (d *Dir) Read(p string) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.Root, filepath.FromSlash(p)))
}
func (d *Dir) IsLink(p string) (string, bool) { t, ok := d.links[p]; return t, ok }

// Git is a repository read from a git object store at a revision, without a
// checkout. With a blobless clone only the files actually read are fetched.
type Git struct {
	GitDir string
	Rev    string
	files  []string
	links  map[string]string
}

// OpenGit lists the tree at rev.
func OpenGit(gitDir, rev string) (*Git, error) {
	g := &Git{GitDir: gitDir, Rev: rev, links: map[string]string{}}
	out, err := exec.Command("git", "--git-dir", gitDir, "ls-tree", "-r", "-t", "-z", "--full-tree", rev).Output()
	if err != nil {
		return nil, err
	}
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok {
			continue
		}
		f := strings.Fields(string(meta)) // mode type sha
		p := string(name)
		skip := false
		for _, part := range strings.Split(p, "/") {
			if skipDirs[part] {
				skip = true
			}
		}
		if skip {
			continue
		}
		switch {
		case len(f) == 3 && f[1] == "tree":
			g.files = append(g.files, p+"/")
		case len(f) == 3 && f[0] == "120000":
			g.links[p] = "" // resolved lazily
			g.files = append(g.files, p)
		default:
			g.files = append(g.files, p)
		}
	}
	sort.Strings(g.files)
	return g, nil
}

func (g *Git) Files() []string { return g.files }
func (g *Git) Read(p string) ([]byte, error) {
	return exec.Command("git", "--git-dir", g.GitDir, "cat-file", "blob", g.Rev+":"+p).Output()
}
func (g *Git) IsLink(p string) (string, bool) {
	if _, ok := g.links[p]; !ok {
		return "", false
	}
	b, err := g.Read(p)
	if err != nil {
		return "", true
	}
	return string(b), true
}

// index answers "does this path exist" quickly.
type index struct {
	set    map[string]bool
	suffix map[string]bool // every trailing part of every path: a/b/c -> b/c, c
	list   []string
	// files written by package.json scripts and Makefiles, built on first need
	outputs []string
}

func newIndex(r Repo) *index {
	ix := &index{set: map[string]bool{}, suffix: map[string]bool{}, list: r.Files()}
	for _, f := range ix.list {
		f = strings.TrimSuffix(f, "/")
		ix.set[f] = true
		for i := 0; i < len(f); i++ {
			if f[i] == '/' {
				ix.suffix[f[i+1:]] = true
			}
		}
	}
	return ix
}

func (ix *index) exists(p string) bool {
	p = path.Clean(strings.TrimSuffix(p, "/"))
	return p == "." || ix.set[p]
}

// anywhere reports whether p exists as a trailing part of any path, so
// "workbench/browser" matches "src/vs/workbench/browser".
func (ix *index) anywhere(p string) bool {
	p = strings.TrimPrefix(path.Clean(strings.TrimSuffix(p, "/")), "./")
	for strings.HasPrefix(p, "../") {
		p = strings.TrimPrefix(p, "../")
	}
	return ix.set[p] || ix.suffix[p]
}

// globAnywhere matches the pattern against every trailing part of every path.
func (ix *index) globAnywhere(pat string) bool {
	pat = strings.TrimPrefix(path.Clean(pat), "./")
	if strings.Contains(pat, "**") {
		pat = strings.ReplaceAll(pat, "**/", "")
		pat = strings.ReplaceAll(pat, "**", "*")
	}
	depth := strings.Count(pat, "/")
	for _, f := range ix.list {
		f = strings.TrimSuffix(f, "/")
		parts := strings.Split(f, "/")
		if len(parts) <= depth {
			continue
		}
		for i := 0; i+depth < len(parts); i++ {
			if ok, _ := path.Match(pat, strings.Join(parts[i:i+depth+1], "/")); ok {
				return true
			}
		}
	}
	return false
}

// glob reports whether any file matches the pattern (supports ** loosely).
func (ix *index) glob(pat string) bool {
	pat = strings.TrimPrefix(path.Clean(pat), "./")
	if strings.Contains(pat, "**") {
		pre, post, _ := strings.Cut(pat, "**")
		post = strings.TrimPrefix(post, "/")
		for _, f := range ix.list {
			f = strings.TrimSuffix(f, "/")
			if !strings.HasPrefix(f, pre) {
				continue
			}
			if post == "" {
				return true
			}
			if ok, _ := path.Match(post, path.Base(f)); ok {
				return true
			}
		}
		return false
	}
	for _, f := range ix.list {
		if ok, _ := path.Match(pat, strings.TrimSuffix(f, "/")); ok {
			return true
		}
	}
	return false
}
