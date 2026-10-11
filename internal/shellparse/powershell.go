package shellparse

import (
	"strings"
)

// PowerShell analyses a PowerShell command line, as Claude Code's PowerShell
// tool and Codex on Windows run them (Phase 0). There is no PowerShell parser
// for Go, so this tokenises the command and knows the cmdlets, aliases and
// parameters that write files
// (https://learn.microsoft.com/powershell/module/microsoft.powershell.management/).
func PowerShell(cmd string) Result {
	w := &walker{vars: map[string]string{}, seen: map[string]bool{}}
	for _, stmt := range psStatements(cmd) {
		w.psStatement(stmt)
	}
	return Result{Targets: w.targets, Opaque: w.opaque}
}

// psWriter describes a writing cmdlet: the parameters that name written
// paths, and whether the first positional argument is a written path.
type psWriter struct {
	params     []string // full parameter names, lower case
	positional int      // how many leading positional arguments are paths (0 to 2)
}

var psWriters = map[string]psWriter{
	"set-content":   {[]string{"path", "literalpath"}, 1},
	"add-content":   {[]string{"path", "literalpath"}, 1},
	"clear-content": {[]string{"path", "literalpath"}, 1},
	"out-file":      {[]string{"filepath", "literalpath"}, 1},
	"tee-object":    {[]string{"filepath", "literalpath"}, 0},
	"new-item":      {[]string{"path"}, 1},
	"rename-item":   {[]string{"path", "literalpath"}, 1},
	"remove-item":   {[]string{"path", "literalpath"}, 1},
	"copy-item":     {[]string{"destination"}, 0}, // the second positional, handled below
	"move-item":     {[]string{"path", "literalpath", "destination"}, 2},
}

// psAliases maps built-in Windows PowerShell aliases to cmdlets.
var psAliases = map[string]string{
	"sc": "set-content", "ac": "add-content", "clc": "clear-content", "tee": "tee-object",
	"ni": "new-item", "ren": "rename-item", "rni": "rename-item",
	"rm": "remove-item", "del": "remove-item", "erase": "remove-item", "ri": "remove-item", "rd": "remove-item", "rmdir": "remove-item",
	"cp": "copy-item", "copy": "copy-item", "cpi": "copy-item",
	"mv": "move-item", "move": "move-item", "mi": "move-item",
}

// psSwitches take no value, so the token after them is not their argument.
var psSwitches = map[string]bool{
	"force": true, "recurse": true, "nonewline": true, "append": true, "whatif": true, "confirm": true,
	"passthru": true, "noclobber": true, "asbytestream": true, "verbose": true,
}

// psLocation changes the current directory.
var psLocation = map[string]bool{"cd": true, "chdir": true, "set-location": true, "sl": true, "pushd": true, "push-location": true}

// psCd follows Set-Location so later relative paths resolve correctly.
func (w *walker) psCd(args []psToken) {
	for i := 0; i < len(args); i++ {
		t := args[i]
		if t.param {
			if strings.HasPrefix(strings.ToLower(t.text), "-p") || strings.HasPrefix(strings.ToLower(t.text), "-l") {
				if i+1 < len(args) {
					w.psCdTo(args[i+1])
				}
				return
			}
			continue
		}
		w.psCdTo(t)
		return
	}
}

func (w *walker) psCdTo(t psToken) {
	if !t.literal {
		w.opaque = true
		return
	}
	dir := strings.ReplaceAll(t.text, `\`, "/")
	if len(dir) >= 2 && dir[1] == ':' { // C:\elsewhere
		w.opaque = true
		w.dir = dir
		return
	}
	w.cd(dir)
}

var psOpaque = map[string]bool{
	"invoke-expression": true, "iex": true, "start-process": true, "saps": true, "start": true,
	"python": true, "python3": true, "py": true, "node": true, "cmd": true, "pwsh": true, "powershell": true,
	"bash": true, "npm": true, "npx": true, "make": true, "git": true, "&": true, ".": true,
}

func (w *walker) psStatement(toks []psToken) {
	if len(toks) == 0 {
		return
	}
	// Redirections anywhere in the statement: > >> 2> *> and friends.
	var rest []psToken
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.redirect {
			if i+1 < len(toks) && !toks[i+1].redirect {
				w.psAdd(toks[i+1])
				i++
			}
			continue
		}
		rest = append(rest, t)
	}
	if len(rest) == 0 {
		return
	}
	// $f = 'a.txt'
	if len(rest) >= 3 && strings.HasPrefix(rest[0].text, "$") && rest[1].text == "=" {
		name := strings.ToLower(strings.TrimPrefix(rest[0].text, "$"))
		if len(rest) == 3 && rest[2].literal {
			w.vars[name] = rest[2].text
		} else {
			delete(w.vars, name)
			if strings.Contains(strings.ToLower(rest[2].text), "::") {
				w.opaque = true
			}
		}
		return
	}
	name := strings.ToLower(rest[0].text)
	if strings.Contains(name, "::") || strings.HasPrefix(name, "[") {
		w.opaque = true // [IO.File]::WriteAllText(...)
		return
	}
	if a, ok := psAliases[name]; ok {
		name = a
	}
	if strings.ContainsAny(name, `\/`) || strings.HasSuffix(name, ".ps1") || psOpaque[name] {
		w.opaque = true
		return
	}
	if psLocation[name] {
		w.psCd(rest[1:])
		return
	}
	spec, ok := psWriters[name]
	if !ok {
		return
	}
	var positional []psToken
	args := rest[1:]
	for i := 0; i < len(args); i++ {
		t := args[i]
		if !t.param {
			positional = append(positional, t)
			continue
		}
		pname, value, hasValue := strings.Cut(strings.ToLower(strings.TrimPrefix(t.text, "-")), ":")
		full := psParam(pname, spec.params)
		var val psToken
		switch {
		case hasValue:
			val = psToken{text: value, literal: true}
		case psSwitches[pname] || i+1 >= len(args) || args[i+1].param:
			continue
		default:
			val = args[i+1]
			i++
		}
		if full != "" {
			w.psAdd(val)
		}
	}
	n := spec.positional
	if name == "copy-item" && len(positional) >= 2 {
		w.psAdd(positional[1]) // Copy-Item <source> <destination>
	}
	for i := 0; i < n && i < len(positional); i++ {
		w.psAdd(positional[i])
	}
}

// psParam resolves a possibly abbreviated parameter name against the
// writer's path parameters, as PowerShell accepts unique prefixes.
func psParam(name string, params []string) string {
	for _, p := range params {
		if name != "" && strings.HasPrefix(p, name) {
			return p
		}
	}
	return ""
}

func (w *walker) psAdd(t psToken) {
	switch {
	case t.literal:
		w.add(t.text)
	case strings.HasPrefix(t.text, "$") && !strings.ContainsAny(t.text, "().:["):
		if v, ok := w.vars[strings.ToLower(t.text[1:])]; ok {
			w.add(v)
		} else {
			w.opaque = true
		}
	default:
		w.opaque = true // (Join-Path ...), "$dir\file", $env:X
	}
}
