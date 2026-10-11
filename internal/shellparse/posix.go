// Package shellparse finds the files a shell command writes, before it runs,
// so `crewbo hook` can deny writes to claimed files that bypass the agents'
// edit tools (`sed -i`, redirects, heredocs). Detection is best effort
// (Phase 0 S3: 71% of writes from the parser, 85% with the daemon's
// literal-name check); the post-run hash check catches the rest.
package shellparse

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Result is what a command writes, as far as static analysis can tell.
type Result struct {
	// Targets are written paths with "/" separators, relative to the
	// command's starting directory (or absolute), after `cd` and simple
	// variable assignments earlier in the same command.
	Targets []string
	// Opaque means the command runs something whose writes cannot be seen
	// statically (an interpreter, a script, `patch`, `xargs`), or writes to
	// a computed path. The daemon then denies if the command mentions a
	// claimed file literally.
	Opaque bool
}

// opaquePrograms can write files in ways the parser cannot see.
var opaquePrograms = map[string]bool{
	"python": true, "python3": true, "node": true, "deno": true, "bun": true, "ruby": true, "php": true,
	"bash": true, "sh": true, "zsh": true, "fish": true, "pwsh": true,
	"make": true, "npm": true, "npx": true, "pnpm": true, "yarn": true, "cargo": true,
	"patch": true, "xargs": true, "find": true, "ed": true, "ex": true, "vi": true, "vim": true, "nvim": true,
	"eval": true, "source": true, ".": true,
}

// POSIX analyses a POSIX or bash command line.
func POSIX(cmd string) Result {
	f, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		return Result{Opaque: true}
	}
	w := &walker{vars: map[string]string{}, seen: map[string]bool{}}
	w.stmts(f.Stmts)
	return Result{Targets: w.targets, Opaque: w.opaque}
}

type walker struct {
	dir     string // current directory relative to the start, "" for the start
	vars    map[string]string
	targets []string
	seen    map[string]bool
	opaque  bool
}

func (w *walker) stmts(list []*syntax.Stmt) {
	for _, s := range list {
		w.stmt(s)
	}
}

func (w *walker) stmt(s *syntax.Stmt) {
	for _, r := range s.Redirs {
		switch r.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll, syntax.RdrClob, syntax.RdrInOut:
			if t, ok := w.word(r.Word); ok {
				if t != "/dev/null" {
					w.add(t)
				}
			} else {
				w.opaque = true
			}
		}
	}
	switch c := s.Cmd.(type) {
	case nil:
	case *syntax.CallExpr:
		w.call(c)
	case *syntax.BinaryCmd:
		w.stmt(c.X)
		w.stmt(c.Y)
	case *syntax.Block:
		w.stmts(c.Stmts)
	case *syntax.Subshell:
		dir := w.dir
		w.stmts(c.Stmts)
		w.dir = dir // cd inside ( ) does not leak
	default:
		// if, for, while, case, functions: check every nested statement
		// with the current state.
		syntax.Walk(c, func(n syntax.Node) bool {
			if st, ok := n.(*syntax.Stmt); ok {
				w.stmt(st)
				return false
			}
			return true
		})
	}
}

func (w *walker) call(c *syntax.CallExpr) {
	if len(c.Args) == 0 {
		for _, a := range c.Assigns {
			if a.Name == nil {
				continue
			}
			if v, ok := w.word(a.Value); ok && a.Value != nil {
				w.vars[a.Name.Value] = v
			} else {
				delete(w.vars, a.Name.Value)
			}
		}
		return
	}
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		v, ok := w.word(a)
		if !ok {
			v = computed // only matters if a writer uses it as a target
		}
		args[i] = v
	}
	if args[0] == computed {
		w.opaque = true
		return
	}
	name := path.Base(args[0])
	if strings.Contains(args[0], "/") && !strings.HasPrefix(args[0], "/usr/") && !strings.HasPrefix(args[0], "/bin/") {
		w.opaque = true // ./script.sh, scripts/gen
		return
	}
	w.command(name, args[1:])
}

func (w *walker) command(name string, args []string) {
	ops, flags := split(args)
	switch name {
	case "cd":
		if len(ops) == 1 {
			if ops[0] == computed {
				w.opaque = true
				return
			}
			w.cd(ops[0])
		}
	case "sed":
		if hasShort(flags, 'i') || hasLong(flags, "--in-place") {
			w.addAll(scriptOperands(args, map[string]bool{"-e": true, "-f": true, "--expression": true, "--file": true, "-l": true}))
		}
	case "perl":
		if hasShort(flags, 'i') {
			w.addAll(scriptOperands(args, map[string]bool{"-e": true, "-E": true}))
		} else {
			w.opaque = true
		}
	case "awk", "gawk":
		if inplace := valueOf(args, "-i"); inplace == "inplace" {
			w.addAll(scriptOperands(args, map[string]bool{"-i": true, "-f": true, "-v": true}))
		} else {
			w.opaque = true
		}
	case "tee", "rm", "touch", "unlink", "shred":
		w.addAll(ops)
	case "truncate":
		w.addAll(withoutValues(args, map[string]bool{"-s": true, "-r": true}))
	case "mv":
		w.addAll(ops)
	case "cp", "install", "ln", "rsync":
		if valueOf(args, "-t") != "" {
			w.opaque = true
		} else if len(ops) >= 2 {
			w.add(ops[len(ops)-1])
		}
	case "dd":
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				w.add(v)
			}
		}
	case "git":
		w.git(ops)
	case "gofmt", "goimports":
		if hasShort(flags, 'w') {
			w.addAll(ops)
		}
	case "prettier":
		if hasLong(flags, "--write") || hasShort(flags, 'w') {
			w.addAll(ops)
		}
	case "clang-format":
		if hasShort(flags, 'i') {
			w.addAll(ops)
		}
	default:
		if opaquePrograms[name] {
			w.opaque = true
		}
	}
}

func (w *walker) git(ops []string) {
	if len(ops) == 0 {
		return
	}
	switch ops[0] {
	case "checkout", "restore", "rm", "mv":
		// `git checkout main` names a branch, not a file; listing it anyway
		// only matters if a claimed file has the same name.
		w.addAll(ops[1:])
	case "apply", "am", "stash", "reset", "clean", "pull", "merge", "rebase", "switch", "cherry-pick", "revert":
		w.opaque = true
	}
}

func (w *walker) cd(dir string) {
	switch {
	case strings.HasPrefix(dir, "/"), dir == "-", strings.HasPrefix(dir, "~"):
		w.opaque = true // leaves the known tree; later relative targets are guesses
		w.dir = path.Clean(dir)
	default:
		w.dir = path.Clean(path.Join(w.dir, dir))
	}
}

// computed stands for an argument whose value is not known statically.
const computed = "\x00computed"

func (w *walker) add(p string) {
	if p == computed {
		w.opaque = true // a write to a path we cannot know
		return
	}
	if strings.TrimSpace(p) == "" || p == "-" {
		return // "-" is stdout; whitespace-only names come from fuzzing
	}
	if !strings.HasPrefix(p, "/") && w.dir != "" && w.dir != "." {
		p = path.Join(w.dir, p)
	}
	if !w.seen[p] {
		w.seen[p] = true
		w.targets = append(w.targets, p)
	}
}

func (w *walker) addAll(ps []string) {
	for _, p := range ps {
		w.add(p)
	}
}

// word returns the literal value of a word, expanding simple variables set
// earlier in the command. ok is false when any part is computed.
func (w *walker) word(wd *syntax.Word) (string, bool) {
	if wd == nil {
		return "", true
	}
	var b strings.Builder
	var part func(p syntax.WordPart) bool
	part = func(p syntax.WordPart) bool {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				if !part(q) {
					return false
				}
			}
		case *syntax.ParamExp:
			if x.Param == nil || x.Excl || x.Length || x.Index != nil || x.Slice != nil || x.Repl != nil || x.Exp != nil {
				return false
			}
			v, ok := w.vars[x.Param.Value]
			if !ok {
				return false
			}
			b.WriteString(v)
		default:
			return false // $(...), arithmetic, globs from brace expansion
		}
		return true
	}
	for _, p := range wd.Parts {
		if !part(p) {
			return "", false
		}
	}
	return b.String(), true
}
