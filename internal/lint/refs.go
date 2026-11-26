package lint

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Words that start a shell command we know how to check, or that make an
// inline code span read as a command rather than a path.
var commandStarts = map[string]bool{
	"npm": true, "pnpm": true, "yarn": true, "bun": true, "npx": true, "make": true, "just": true, "task": true,
	"go": true, "cargo": true, "python": true, "python3": true, "pytest": true, "uv": true, "poetry": true,
	"pip": true, "pip3": true, "bash": true, "sh": true, "node": true, "cd": true, "tox": true, "nox": true,
	"mvn": true, "gradle": true, "./gradlew": true, "docker": true, "git": true, "deno": true, "rake": true,
	"bundle": true, "dotnet": true, "mix": true, "hatch": true, "pdm": true, "nx": true, "turbo": true,
}

func looksLikeCommand(s string) bool {
	f := strings.Fields(strings.TrimPrefix(s, "$ "))
	if len(f) < 2 {
		return false
	}
	return commandStarts[f[0]] || strings.HasPrefix(f[0], "./") && strings.Contains(f[0], ".sh")
}

var promptRe = regexp.MustCompile(`^\s*(\$|>|#\s|PS>|%)\s*`)

// command checks one shell line: scripts, make targets, recipes, paths.
func (c *checker) command(line int, raw string) {
	s := strings.TrimSpace(promptRe.ReplaceAllString(raw, ""))
	if s == "" || strings.HasPrefix(s, "#") {
		return
	}
	if i := strings.Index(s, " #"); i > 0 {
		s = s[:i]
	}
	cwd := ""
	for _, part := range splitChain(s) {
		f := strings.Fields(part)
		for len(f) > 0 && strings.Contains(f[0], "=") && !strings.HasPrefix(f[0], "-") { // FOO=bar cmd
			f = f[1:]
		}
		if len(f) == 0 {
			continue
		}
		if f[0] == "cd" && len(f) > 1 {
			if !strings.Contains(strings.Trim(f[1], "./"), "/") {
				cwd = path.Join(cwd, f[1]) // "cd repo" after a clone: not checkable
				continue
			}
			c.paths++
			if !c.pathExists(f[1], cwd) && !placeholder(f[1]) && !strings.HasPrefix(f[1], "~") && !strings.HasPrefix(f[1], "/") && f[1] != "-" && !strings.Contains(f[1], "$") {
				c.add(line, "missing-path", Error, "cd into a directory that does not exist", part)
			}
			cwd = path.Join(cwd, f[1])
			continue
		}
		c.commands++
		c.oneCommand(line, f, part, cwd)
	}
}

func splitChain(s string) []string {
	var out []string
	for _, a := range regexp.MustCompile(`\s*(&&|\|\||;|\|)\s*`).Split(s, -1) {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

func (c *checker) notePM(pm string, line int) {
	if c.pms == nil {
		c.pms = map[string]int{}
	}
	if _, ok := c.pms[pm]; !ok {
		c.pms[pm] = line
	}
}

var npmBuiltins = map[string]bool{"install": true, "i": true, "ci": true, "add": true, "remove": true, "rm": true, "uninstall": true,
	"exec": true, "dlx": true, "init": true, "create": true, "publish": true, "link": true, "update": true, "up": true, "upgrade": true, "outdated": true,
	"audit": true, "why": true, "list": true, "ls": true, "pack": true, "version": true, "config": true, "set": true, "info": true, "view": true,
	"login": true, "logout": true, "cache": true, "help": true, "store": true, "prune": true, "dedupe": true, "import": true, "workspace": true,
	"workspaces": true, "x": true, "patch": true, "env": true, "global": true, "test": false, "start": false, "--version": true, "-v": true, "node": true,
}

func (c *checker) oneCommand(line int, f []string, text, cwd string) {
	cmd := f[0]
	args := f[1:]
	switch cmd {
	case "npm", "pnpm", "yarn", "bun":
		c.notePM(cmd, line)
		if hasFlag(args, "--filter", "-F", "--workspace", "-w", "--prefix", "-C", "--cwd", "--dir") || (cmd == "yarn" && len(args) > 0 && args[0] == "workspace") {
			return // runs in another package; too dynamic to check here
		}
		script := ""
		switch {
		case len(args) >= 2 && (args[0] == "run" || args[0] == "run-script"):
			script = args[1]
		case len(args) >= 1 && (args[0] == "test" || args[0] == "t" || args[0] == "start") && cmd == "npm":
			script = map[string]string{"t": "test"}[args[0]]
			if script == "" {
				script = args[0]
			}
		case len(args) >= 1 && cmd != "npm" && !npmBuiltins[args[0]] && !strings.HasPrefix(args[0], "-"):
			script = args[0] // pnpm lint, yarn build, bun test
			if cmd == "bun" && (script == "test" || script == "run" || strings.Contains(script, ".")) {
				return // bun test is built in; bun file.ts runs a file
			}
		}
		if script == "" || placeholder(script) || strings.ContainsAny(script, "*?") {
			return
		}
		scripts, pj, ok := c.scripts(cwd)
		if ok {
			if _, has := scripts[script]; has {
				return
			}
		}
		if cmd == "yarn" || cmd == "pnpm" || cmd == "bun" { // could be a binary from node_modules/.bin
			switch script {
			case "tsc", "eslint", "prettier", "jest", "vitest", "playwright", "biome", "turbo", "nx", "tsx", "changeset", "lerna", "knip", "oxlint", "vite", "next", "astro", "storybook", "drizzle-kit", "prisma", "wrangler":
				return
			}
		}
		// pnpm/yarn/bun X also runs the binary of an installed package.
		if cmd != "npm" && c.isDependency(script) {
			return
		}
		// The instructions may describe a step to run inside another package
		// of a monorepo; accept the script if any package.json defines it.
		if where := c.anyPackageScript(script); where != "" {
			return
		}
		if !ok {
			c.add(line, "missing-script", Error, fmt.Sprintf("no package.json in the repository defines a %q script", script), text)
			return
		}
		c.add(line, "missing-script", Error, fmt.Sprintf("no package.json in the repository defines a %q script (nearest, %s, has: %s)", script, pj, keys(scripts, 8)), text)
	case "make":
		targets, mf, ok := c.makeTargets(cwd, flagValue(args, "-C"))
		if ok && targets == nil { // pattern or included rules: cannot be sure
			return
		}
		for _, a := range args {
			if strings.HasPrefix(a, "-") || strings.Contains(a, "=") || placeholder(a) || a == flagValue(args, "-C") {
				continue
			}
			if ok && targets[a] {
				continue
			}
			if c.anyMakeTarget(a) {
				continue
			}
			if !ok {
				c.add(line, "missing-make-target", Error, fmt.Sprintf("no Makefile in the repository has a target %q", a), text)
			} else {
				c.add(line, "missing-make-target", Error, fmt.Sprintf("%s has no target %q, and no other Makefile does", mf, a), text)
			}
		}
	case "just":
		if len(args) == 0 || strings.HasPrefix(args[0], "-") || placeholder(args[0]) {
			return
		}
		recipes, ok := c.justRecipes(cwd)
		if ok && (len(recipes) == 0 || recipes[args[0]]) {
			return
		}
		if c.anyJustRecipe(args[0]) {
			return
		}
		if !ok {
			c.add(line, "missing-just-recipe", Error, fmt.Sprintf("no justfile in the repository has a recipe %q", args[0]), text)
		} else {
			c.add(line, "missing-just-recipe", Error, fmt.Sprintf("justfile has no recipe %q, and no other justfile does", args[0]), text)
		}
	case "go":
		if placeholder(text) {
			return
		}
		if len(args) > 1 && (args[0] == "test" || args[0] == "build" || args[0] == "run" || args[0] == "vet" || args[0] == "generate") {
			for _, a := range args[1:] {
				if strings.HasPrefix(a, "./") {
					p := strings.TrimSuffix(strings.TrimSuffix(a, "/..."), "...")
					if p != "./" && p != "." && !c.pathExists(p, cwd) {
						c.add(line, "missing-path", Error, "Go package path does not exist", text)
					}
				}
			}
		}
	case "pytest", "python", "python3", "node", "bash", "sh", "deno", "tsx", "ts-node":
		if placeholder(text) || strings.Contains(text, "/path/to") {
			return
		}
		for _, a := range args {
			if strings.HasPrefix(a, "-") || placeholder(a) || strings.Contains(a, "=") {
				if a == "-m" || a == "-c" {
					return
				}
				continue
			}
			p := strings.SplitN(a, "::", 2)[0]
			if looksLikePath(p) && !c.pathExists(p, cwd) && !c.ix.glob(path.Join(c.base(cwd), p)) {
				c.add(line, "missing-path", Error, "file passed to "+cmd+" does not exist", text)
			}
			if cmd != "pytest" {
				break // only the script argument
			}
		}
	case "poetry":
		c.notePM("poetry", line)
	case "uv":
		c.notePM("uv", line)
	case "pip", "pip3":
		if i := indexOf(args, "-r"); i >= 0 && i+1 < len(args) && !c.pathExists(args[i+1], cwd) {
			c.add(line, "missing-path", Error, "requirements file does not exist", text)
		}
	default:
		if strings.HasPrefix(cmd, "./") && !strings.Contains(cmd[2:], "/") && !strings.Contains(cmd[2:], ".") {
			return // ./binary: usually something the build produces
		}
		if strings.HasPrefix(cmd, "./") || strings.HasPrefix(cmd, "scripts/") || strings.HasPrefix(cmd, "bin/") || strings.HasPrefix(cmd, "hack/") {
			if !c.pathExists(cmd, cwd) && !placeholder(cmd) && !strings.HasPrefix(cmd, "./node_modules") && !strings.HasPrefix(cmd, "./target") && !strings.HasPrefix(cmd, "./bin/") && !strings.HasPrefix(cmd, "./build") && !strings.HasPrefix(cmd, "./dist") {
				c.add(line, "missing-path", Error, "script does not exist", text)
			}
		}
	}
}

// pathRef checks an inline code span or link target that may be a path.
// It is deliberately conservative: a span counts as a path only if it has a
// file extension, a trailing slash, or starts with ./ or ../, and it is
// accepted if it exists relative to the file, the root, or as the tail of
// any path in the repository.
func (c *checker) pathRef(line int, s string, isLink bool) {
	s = strings.Trim(s, "'\"")
	s = strings.TrimPrefix(s, "mdc:") // Cursor rule links
	if i := strings.Index(s, "::"); i > 0 {
		s = s[:i] // file.py::function
	}
	if i := strings.IndexAny(s, "#?"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, ":"), ",")
	if m := lineSuffixRe.FindStringSubmatch(s); m != nil {
		s = m[1] // file.go:123 or file.go:Func
	}
	if isLink && (strings.Contains(s, "://") || strings.HasPrefix(s, "mailto:") || s == "") {
		return
	}
	if !looksLikePath(s) || placeholder(s) || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, "$") {
		return
	}
	if hostRe.MatchString(s) || strings.Contains(s, "\\ ") {
		return // example.com/path written without https://, or an escaped space
	}
	stem := strings.Trim(s, "./")
	if !strings.Contains(stem, "/") && !strings.HasPrefix(s, "./") && !strings.HasPrefix(s, "../") && !isLink {
		return // a bare name like utils.py may describe a kind of file, not a path
	}
	if knownGenerated(s) || c.ignored(s) || userLocal(s) || c.escapes(s) || c.inSubmodule(s) {
		return
	}
	c.paths++
	if strings.ContainsAny(s, "*?[") {
		if !c.ix.glob(path.Join(c.dir, s)) && !c.ix.glob(s) && !c.ix.globAnywhere(s) {
			c.add(line, "missing-path", Warn, "no file matches this pattern", s)
		}
		return
	}
	if c.pathExists(s, "") || c.writtenByScript(s) {
		return
	}
	c.add(line, "missing-path", Error, "no such file or directory anywhere in the repository", s)
}

var hostRe = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)*\.(com|org|io|dev|net|ai|co|app|sh|page|xyz)(/|$)`)

var lineSuffixRe = regexp.MustCompile(`^(.+\.\w+):[\w.]+$`)

func (c *checker) base(cwd string) string { return path.Join(c.dir, cwd) }

// pathExists tries the path relative to the instruction file's directory
// (plus any cd), then relative to the repository root.
func (c *checker) pathExists(p, cwd string) bool {
	p = strings.TrimPrefix(p, "./")
	return c.ix.exists(path.Join(c.dir, cwd, p)) || c.ix.exists(path.Join(cwd, p)) || c.ix.exists(p) || c.ix.anywhere(p)
}

var extRe = regexp.MustCompile(`\.(go|py|ts|tsx|js|jsx|mjs|cjs|rs|java|kt|rb|php|cs|c|h|cc|cpp|hpp|swift|md|mdx|json|ya?ml|toml|ini|cfg|sh|sql|proto|txt|lock|env|html|css|scss|vue|svelte|xml|gradle|tf|mdc|ipynb)$`)

func looksLikePath(s string) bool {
	if s == "" || strings.ContainsAny(s, " <>{}()|;=,`'\"@") || strings.Contains(s, "://") || strings.HasPrefix(s, "-") {
		return false
	}
	base := path.Base(strings.TrimSuffix(s, "/"))
	if strings.HasPrefix(base, ".") && strings.Count(base, ".") == 1 && !strings.Contains(s, "/") && extRe.MatchString(base) && len(base) < 7 {
		return false // ".ts", ".md": an extension, not a file
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	if regexp.MustCompile(`^v\d`).MatchString(s) {
		return false
	}
	return extRe.MatchString(s) || (strings.HasSuffix(s, "/") && strings.Count(s, "/") >= 2)
}

var placeholderRe = regexp.MustCompile(`(?i)<[^>]*>|\{[^}]*\}|\$\{?\w|\bxxx\b|\bfoo\b|\bbar\b|\.\.\.|…|your|example|sample|template|placeholder|componentname|\bmy[-_]|\btest_x|\bname\.|_x\.|/x\.|\bnew[-_]|\bsome[-_]|[-_/.][A-Z]{1,2}([-_.][A-Z0-9])?\.\w+$|\bX\.Y|YYYY|\bNNN`)

func placeholder(s string) bool { return placeholderRe.MatchString(s) }

// escapes reports whether a relative path leaves the repository.
func (c *checker) escapes(s string) bool {
	return strings.HasPrefix(path.Clean(path.Join(c.dir, s)), "..")
}

// inSubmodule reports whether the path is inside a git submodule, whose
// files are not part of this repository's tree.
func (c *checker) inSubmodule(s string) bool {
	if c.submodules == nil {
		c.submodules = []string{}
		if b, err := c.r.Read(".gitmodules"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				l = strings.TrimSpace(l)
				if strings.HasPrefix(l, "path") {
					if _, v, ok := strings.Cut(l, "="); ok {
						c.submodules = append(c.submodules, strings.TrimSpace(v))
					}
				}
			}
		}
	}
	s = strings.TrimPrefix(s, "./")
	for _, m := range c.submodules {
		if s == m || strings.HasPrefix(s, m+"/") || strings.HasPrefix(path.Join(c.dir, s), m+"/") || strings.HasPrefix(s, path.Base(m)+"/") {
			return true
		}
	}
	return false
}

// userLocal: agent configuration that is often installed per user rather
// than committed (skills, local settings), so its absence proves nothing.
func userLocal(s string) bool {
	s = strings.TrimPrefix(s, "./")
	for _, p := range []string{".claude/", ".agents/", ".agent/", ".cursor/", ".codex/", ".gemini/", ".windsurf/", ".opencode/", ".kiro/", ".roo/"} {
		if strings.HasPrefix(s, p) || s == strings.TrimSuffix(p, "/") {
			return true
		}
	}
	return strings.Contains(s, ".local.") || strings.HasSuffix(s, ".local")
}

// ignored reports whether the root .gitignore excludes the path, i.e. the
// file is expected to be generated or created locally.
func (c *checker) ignored(s string) bool {
	if c.gitignore == nil {
		c.gitignore = []string{}
		for _, f := range []string{".gitignore", path.Join(c.dir, ".gitignore")} {
			if b, err := c.r.Read(f); err == nil {
				for _, l := range strings.Split(string(b), "\n") {
					l = strings.TrimSpace(l)
					if l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "!") {
						c.gitignore = append(c.gitignore, strings.TrimSuffix(strings.TrimPrefix(l, "/"), "/"))
					}
				}
			}
		}
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "./"), "/")
	parts := strings.Split(s, "/")
	for _, pat := range c.gitignore {
		if strings.Contains(pat, "/") {
			if ok, _ := path.Match(pat, s); ok || strings.HasPrefix(s, pat+"/") {
				return true
			}
			continue
		}
		for _, part := range parts {
			if ok, _ := path.Match(pat, part); ok {
				return true
			}
		}
	}
	return false
}

func knownGenerated(s string) bool {
	for _, g := range []string{"node_modules", "dist/", "build/", ".env", "target/", "coverage", ".venv", "venv/", "__pycache__", "generated", ".next", "out/", "bin/", "tmp/", ".cache", "vendor/"} {
		if strings.Contains(s, g) {
			return true
		}
	}
	return false
}

// isDependency reports whether any package.json depends on a package named
// name (its binary usually has the same name).
func (c *checker) isDependency(name string) bool {
	if c.deps == nil {
		c.deps = map[string]bool{}
		for _, f := range c.ix.list {
			if path.Base(f) != "package.json" {
				continue
			}
			b, err := c.r.Read(f)
			if err != nil {
				continue
			}
			var pj map[string]json.RawMessage
			if json.Unmarshal(b, &pj) != nil {
				continue
			}
			for _, k := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
				var m map[string]any
				_ = json.Unmarshal(pj[k], &m)
				for d := range m {
					c.deps[d] = true
					c.deps[path.Base(d)] = true // @scope/cli -> cli
				}
			}
		}
	}
	return c.deps[name]
}

// anyPackageScript returns a package.json that defines script, if any does.
func (c *checker) anyPackageScript(script string) string {
	if c.allScripts == nil {
		c.allScripts = map[string]string{}
		for _, f := range c.ix.list {
			if path.Base(f) != "package.json" {
				continue
			}
			b, err := c.r.Read(f)
			if err != nil {
				continue
			}
			var pj struct {
				Scripts map[string]any `json:"scripts"`
			}
			_ = json.Unmarshal(b, &pj)
			for k := range pj.Scripts {
				if _, ok := c.allScripts[k]; !ok {
					c.allScripts[k] = f
				}
			}
		}
	}
	return c.allScripts[script]
}

// anyMakeTarget reports whether any Makefile defines target, or one cannot
// be fully parsed (includes, pattern rules), in which case it gives up.
func (c *checker) anyMakeTarget(target string) bool {
	if c.allTargets == nil {
		c.allTargets = map[string]bool{}
		for _, f := range c.ix.list {
			b := path.Base(f)
			if b != "Makefile" && b != "makefile" && b != "GNUmakefile" && !strings.HasSuffix(b, ".mk") {
				continue
			}
			t, _, _ := c.makeTargets(path.Dir(f), "")
			if t == nil && path.Dir(f) == "." {
				c.allTargets["*"] = true
			}
			for k := range t {
				c.allTargets[k] = true
			}
		}
	}
	return c.allTargets[target] || c.allTargets["*"]
}

// anyJustRecipe reports whether any justfile in the repository defines the
// recipe (or imports others, in which case it gives up).
func (c *checker) anyJustRecipe(name string) bool {
	for _, f := range c.ix.list {
		b := path.Base(f)
		if b != "justfile" && b != "Justfile" && b != ".justfile" && !strings.HasSuffix(b, ".just") {
			continue
		}
		r, ok := c.justRecipes(path.Dir(f))
		if ok && (len(r) == 0 || r[name]) {
			return true
		}
	}
	return false
}

func (c *checker) scripts(cwd string) (map[string]any, string, bool) {
	for _, dir := range []string{path.Join(c.dir, cwd), cwd, c.dir, "."} {
		p := path.Join(dir, "package.json")
		if !c.ix.exists(p) {
			continue
		}
		b, err := c.r.Read(p)
		if err != nil {
			continue
		}
		var pj struct {
			Scripts map[string]any `json:"scripts"`
		}
		_ = json.Unmarshal(b, &pj)
		if pj.Scripts == nil {
			pj.Scripts = map[string]any{}
		}
		return pj.Scripts, p, true
	}
	return nil, "", false
}

var makeTargetRe = regexp.MustCompile(`^([A-Za-z0-9_.\-/][A-Za-z0-9_.\-/ ]*?)\s*::?(?:[^=]|$)`)

func (c *checker) makeTargets(cwd, dashC string) (map[string]bool, string, bool) {
	dirs := []string{path.Join(c.dir, cwd), cwd, c.dir, "."}
	if dashC != "" {
		dirs = []string{path.Join(c.dir, cwd, dashC), path.Join(cwd, dashC), dashC}
	}
	for _, dir := range dirs {
		for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
			p := path.Join(dir, name)
			if !c.ix.exists(p) {
				continue
			}
			b, err := c.r.Read(p)
			if err != nil {
				return nil, p, true
			}
			t := map[string]bool{}
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "include ") || strings.HasPrefix(l, "-include ") || strings.HasPrefix(l, "%:") || strings.Contains(l, "$(eval") {
					return nil, p, true // targets may come from elsewhere
				}
				if l == "" || l[0] == '\t' || l[0] == '#' {
					continue
				}
				if m := makeTargetRe.FindStringSubmatch(l); m != nil {
					for _, x := range strings.Fields(m[1]) {
						t[x] = true
					}
				}
			}
			if len(t) == 0 {
				return nil, p, true
			}
			return t, p, true
		}
	}
	return nil, "", false
}

var recipeRe = regexp.MustCompile(`^@?([A-Za-z0-9_-]+)(\s[^:=]*)?:([^=]|$)`)

func (c *checker) justRecipes(cwd string) (map[string]bool, bool) {
	for _, dir := range []string{path.Join(c.dir, cwd), cwd, c.dir, "."} {
		for _, name := range []string{"justfile", "Justfile", ".justfile"} {
			p := path.Join(dir, name)
			if !c.ix.exists(p) {
				continue
			}
			b, _ := c.r.Read(p)
			r := map[string]bool{}
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "import ") || strings.HasPrefix(l, "mod ") {
					return map[string]bool{}, true
				}
				if m := recipeRe.FindStringSubmatch(l); m != nil {
					r[m[1]] = true
				}
				if strings.HasPrefix(l, "alias ") {
					if f := strings.Fields(l); len(f) > 1 {
						r[f[1]] = true
					}
				}
			}
			return r, true
		}
	}
	return nil, false
}

// packageManager flags instructions that use a different package manager
// from the lockfile the repository actually commits.
func (c *checker) packageManager() {
	locks := map[string]string{"npm": "package-lock.json", "pnpm": "pnpm-lock.yaml", "yarn": "yarn.lock", "bun": "bun.lockb"}
	var have []string
	for pm, lock := range locks {
		if c.ix.exists(path.Join(c.dir, lock)) || c.ix.exists(lock) || (pm == "bun" && (c.ix.exists("bun.lock") || c.ix.exists(path.Join(c.dir, "bun.lock")))) {
			have = append(have, pm)
		}
	}
	if len(have) != 1 {
		return
	}
	for pm, line := range c.pms {
		if _, js := locks[pm]; js && pm != have[0] {
			c.add(line, "wrong-package-manager", Warn, fmt.Sprintf("uses %s, but the repository commits a %s lockfile (%s)", pm, have[0], locks[have[0]]), pm)
		}
	}
	if line, ok := c.pms["poetry"]; ok && !c.ix.exists("poetry.lock") && !c.ix.exists(path.Join(c.dir, "poetry.lock")) && (c.ix.exists("uv.lock") || c.ix.exists(path.Join(c.dir, "uv.lock"))) {
		c.add(line, "wrong-package-manager", Warn, "uses poetry, but the repository commits uv.lock and no poetry.lock", "poetry")
	}
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name) && len(a) > len(name) {
			return strings.TrimPrefix(a[len(name):], "=")
		}
	}
	return ""
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func keys(m map[string]any, max int) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	if len(ks) == 0 {
		return "none"
	}
	sort.Strings(ks)
	if len(ks) > max {
		return strings.Join(ks[:max], ", ") + ", …"
	}
	return strings.Join(ks, ", ")
}

// outputRe finds files a build script writes: `cmd > out.ts`, `-o out`, `--out-file out`, `--output=out`.
var outputRe = regexp.MustCompile(`(?:>{1,2}|\s-o|--out(?:put)?(?:-?file|-?dir)?(?:=|\s))\s*["']?(\.{0,2}/?[\w@.\-/]+)`)

// writtenByScript reports whether some package.json script or Makefile recipe
// writes the path, so it exists after a build even though it is not committed.
// The list is built once per repository (it lives on the shared index) and only
// when a path would otherwise be reported, because reading every manifest in a
// blobless clone fetches each one over the network.
func (c *checker) writtenByScript(s string) bool {
	if c.outputs == nil && c.ix.outputs != nil {
		c.outputs = c.ix.outputs
	}
	if c.outputs == nil {
		defer func() { c.ix.outputs = c.outputs }()
		c.outputs = []string{}
		for _, f := range c.ix.list {
			b := path.Base(f)
			var texts []string
			switch {
			case b == "package.json":
				raw, err := c.r.Read(f)
				if err != nil {
					continue
				}
				var pj struct {
					Scripts map[string]any `json:"scripts"`
				}
				_ = json.Unmarshal(raw, &pj)
				for _, v := range pj.Scripts {
					if str, ok := v.(string); ok {
						texts = append(texts, str)
					}
				}
			case b == "Makefile" || b == "makefile" || b == "GNUmakefile" || b == "justfile" || b == "Justfile":
				raw, err := c.r.Read(f)
				if err != nil {
					continue
				}
				texts = append(texts, string(raw))
			default:
				continue
			}
			dir := path.Dir(f)
			for _, t := range texts {
				for _, m := range outputRe.FindAllStringSubmatch(t, -1) {
					out := strings.TrimSuffix(m[1], "/")
					if out == "" || out == "/dev/null" || strings.HasPrefix(out, "/") {
						continue
					}
					c.outputs = append(c.outputs, path.Clean(out), path.Clean(path.Join(dir, out)))
				}
			}
		}
	}
	p := path.Clean(strings.TrimPrefix(strings.TrimSuffix(s, "/"), "./"))
	for _, o := range c.outputs {
		if o == p || strings.HasSuffix(o, "/"+p) || strings.HasPrefix(o, p+"/") {
			return true
		}
	}
	return false
}
