package shellparse

import "strings"

// psToken is one PowerShell token.
type psToken struct {
	text     string
	literal  bool // a known string value (bare word or quoted without expansion)
	param    bool // -Name or -Name:value
	redirect bool // > >> 2> *> 2>&1 and similar
}

// psStatements splits a command into statements on ; | && || and newlines,
// and each statement into tokens. Quotes: '...' is literal (” is a quote),
// "..." is literal unless it contains $ or a subexpression, ` escapes.
func psStatements(cmd string) [][]psToken {
	var stmts [][]psToken
	var cur []psToken
	flush := func() {
		if len(cur) > 0 {
			stmts = append(stmts, cur)
			cur = nil
		}
	}
	r := []rune(cmd)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '\n' || c == ';':
			flush()
			i++
		case c == '|' || c == '&' && i+1 < len(r) && r[i+1] == '&':
			flush()
			for i < len(r) && (r[i] == '|' || r[i] == '&') {
				i++
			}
		case c == '>' || (c == '2' || c == '*' || c == '1' || c == '3') && i+1 < len(r) && r[i+1] == '>':
			j := i
			for j < len(r) && strings.ContainsRune("123456*>&", r[j]) {
				j++
			}
			op := string(r[i:j])
			i = j
			if strings.HasSuffix(op, "&1") || strings.HasSuffix(op, "&2") {
				continue // stream merge, not a file
			}
			cur = append(cur, psToken{text: op, redirect: true})
		case c == '\'':
			var b strings.Builder
			i++
			for i < len(r) {
				if r[i] == '\'' {
					if i+1 < len(r) && r[i+1] == '\'' {
						b.WriteRune('\'')
						i += 2
						continue
					}
					i++
					break
				}
				b.WriteRune(r[i])
				i++
			}
			cur = append(cur, psToken{text: b.String(), literal: true})
		case c == '"':
			var b strings.Builder
			expands := false
			i++
			for i < len(r) && r[i] != '"' {
				if r[i] == '`' && i+1 < len(r) {
					b.WriteRune(r[i+1])
					i += 2
					continue
				}
				if r[i] == '$' {
					expands = true
				}
				b.WriteRune(r[i])
				i++
			}
			i++
			cur = append(cur, psToken{text: b.String(), literal: !expands})
		case c == '(' || c == '{':
			// A sub-expression or script block: keep it as one opaque token.
			opening, closing := c, map[rune]rune{'(': ')', '{': '}'}[c]
			depth, j := 0, i
			for j < len(r) {
				if r[j] == opening {
					depth++
				} else if r[j] == closing {
					depth--
					if depth == 0 {
						j++
						break
					}
				}
				j++
			}
			cur = append(cur, psToken{text: string(r[i:j])})
			i = j
		default:
			j := i
			for j < len(r) && !strings.ContainsRune(" \t\r\n;|>'\"(){}", r[j]) {
				if r[j] == '`' && j+1 < len(r) {
					j++
				}
				if r[j] == '&' && j+1 < len(r) && r[j+1] == '&' {
					break
				}
				j++
			}
			word := strings.ReplaceAll(string(r[i:j]), "`", "")
			i = j
			switch {
			case strings.HasPrefix(word, "-") && len(word) > 1 && !strings.HasPrefix(word, "--"):
				cur = append(cur, psToken{text: word, param: true})
			case strings.HasPrefix(word, "$"):
				cur = append(cur, psToken{text: word})
			default:
				cur = append(cur, psToken{text: word, literal: true})
			}
		}
	}
	flush()
	return stmts
}
