package shellparse

import "strings"

// split separates operands from flags. A lone "--" ends the flags.
func split(args []string) (ops, flags []string) {
	done := false
	for _, a := range args {
		switch {
		case done:
			ops = append(ops, a)
		case a == "--":
			done = true
		case strings.HasPrefix(a, "-") && a != "-":
			flags = append(flags, a)
		default:
			ops = append(ops, a)
		}
	}
	return ops, flags
}

// hasShort reports a short flag letter, alone or in a cluster ("-i",
// "-i.bak", "-pi", "-Ei").
func hasShort(flags []string, c byte) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, "--") || len(f) < 2 {
			continue
		}
		if i := strings.IndexByte(f[1:], c); i >= 0 {
			return true
		}
	}
	return false
}

func hasLong(flags []string, name string) bool {
	for _, f := range flags {
		if f == name || strings.HasPrefix(f, name+"=") {
			return true
		}
	}
	return false
}

// valueOf returns the value of a flag given as "-x value" or "-xvalue".
func valueOf(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, flag); ok && v != "" && !strings.HasPrefix(flag, "--") && len(flag) == 2 {
			return v
		}
	}
	return ""
}

// withoutValues returns operands, skipping the values of flags that take one.
func withoutValues(args []string, takesValue map[string]bool) []string {
	var ops []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case takesValue[a]:
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			ops = append(ops, a)
		}
	}
	return ops
}

// scriptOperands returns the file operands of a sed-, perl- or awk-style
// command: when a script flag (-e, -f) is present every operand is a file;
// otherwise the first operand is the script.
func scriptOperands(args []string, takesValue map[string]bool) []string {
	script := false
	for _, a := range args {
		if a == "-e" || a == "-E" || a == "-f" || strings.HasPrefix(a, "--expression") || strings.HasPrefix(a, "--file") {
			script = true
		}
	}
	ops := withoutValues(args, takesValue)
	if !script && len(ops) > 0 {
		ops = ops[1:]
	}
	return ops
}
