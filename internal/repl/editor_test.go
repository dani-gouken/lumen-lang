package repl

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// Keys as a terminal sends them.
const (
	up        = "\x1b[A"
	down      = "\x1b[B"
	right     = "\x1b[C"
	left      = "\x1b[D"
	home      = "\x01"
	end       = "\x05"
	backspace = "\x7f"
	del       = "\x1b[3~"
	wordLeft  = "\x1bb"
	altEnter  = "\x1b\r"
)

// typeAt gives an editor the keys, and returns the inputs it read until
// the keys ran out.
func typeAt(t *testing.T, keys string, prepare func(*Editor)) ([]string, *Editor, string) {
	t.Helper()
	var screen bytes.Buffer
	e := NewEditor(strings.NewReader(keys), &screen)
	e.Incomplete = Incomplete
	if prepare != nil {
		prepare(e)
	}
	var inputs []string
	for i := 0; i < 100; i++ {
		input, err := e.ReadInput("> ", ". ")
		if err == ErrInterrupted {
			inputs = append(inputs, "<interrupted>")
			continue
		}
		if err != nil {
			if err != io.EOF {
				t.Fatalf("reading: %v", err)
			}
			return inputs, e, screen.String()
		}
		inputs = append(inputs, input)
		e.Remember(input)
	}
	t.Fatal("the editor never ran out of keys")
	return nil, nil, ""
}

func TestEditorKeys(t *testing.T) {
	cases := []struct {
		name, keys string
		want       []string
	}{
		{"plain", "1 + 2\r", []string{"1 + 2"}},
		{"two inputs", "a\rb\r", []string{"a", "b"}},
		{"backspace", "abx" + backspace + "c\r", []string{"abc"}},
		{"insert in the middle", "ac" + left + "b\r", []string{"abc"}},
		{"home and end", "bc" + home + "a" + end + "d\r", []string{"abcd"}},
		{"delete", "abc" + home + del + "\r", []string{"bc"}},
		{"kill to the end", "abcdef" + left + left + "\x0b\r", []string{"abcd"}},
		{"kill to the start", "abcdef" + left + left + "\x15\r", []string{"ef"}},
		{"kill a word", "let total = 1\x17\x17\r", []string{"let "}},
		{"word left", "one two" + wordLeft + "2 \r", []string{"one 2 two"}},
		{"ctrl-d deletes when there is text", "abc" + home + "\x04\r", []string{"bc"}},
		{"ctrl-c abandons", "abc\x03def\r", []string{"<interrupted>", "def"}},
		{"unicode", "\"héé\"" + left + left + backspace + "e\r", []string{"\"heé\""}},
		{"wide characters", "\"日本\"" + left + left + "x\r", []string{"\"日x本\""}},
		{"unknown sequences are ignored", "a\x1b[15~b\x1bOPc\r", []string{"abc"}},
		{"end of the keys enters what is there", "abc", []string{"abc"}},

		// An open bracket carries the input on to the next line, indented
		{"a block", "fn f() {\rreturn 1;\r}\r", []string{"fn f() {\n    return 1;\n}"}},
		{"nested blocks", "if a {\rif b {\rc();\r}\r}\r", []string{"if a {\n    if b {\n        c();\n    }\n}"}},
		{"an open call", "f(1,\r2)\r", []string{"f(1,\n    2)"}},
		{"alt-enter starts a line", "a;" + altEnter + "b;\r", []string{"a;\nb;"}},
		{"backspace takes a level of indentation", "if a {\r" + backspace + "x\r}\r", []string{"if a {\nx\n}"}},
		{"tab indents at the start of a line", "if a {\r\tb\r}\r", []string{"if a {\n        b\n    }"}},
		{"moving between lines", "f(1,\r2)" + up + home + "g" + down + end + ";\r", []string{"gf(1,\n    2);"}},
		{"an empty input", "\r\rx\r", []string{"", "", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, _ := typeAt(t, c.keys, nil)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("read %q, want %q", got, c.want)
			}
		})
	}
}

func TestEditorHistory(t *testing.T) {
	keys := "first\r" + "second\r" +
		up + up + "!\r" + // first!
		"dra" + up + up + down + down + "ft\r" + // back to what was being typed
		up + up + up + up + up + up + "\r" + // no further than the oldest
		"fn f() {\rreturn 1;\r}\r" +
		"x" + up + up + up + up + "\r" // a whole input comes back; inside it, up moves a line, and past its first to the input before
	got, e, _ := typeAt(t, keys, nil)
	want := []string{"first", "second", "first!", "draft", "first", "fn f() {\n    return 1;\n}", "first"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("read %q\nwant %q", got, want)
	}
	// The same input twice in a row is remembered once
	e.Remember("first")
	e.Remember("  ")
	if n := len(e.History); n != 7 {
		t.Fatalf("history holds %d inputs: %q", n, e.History)
	}
}

func TestEditorCompletion(t *testing.T) {
	complete := func(before string) ([]string, int) {
		word := before[strings.LastIndexAny(before, " .")+1:]
		return matching([]string{"print", "println", "panic", "push"}, word), len(word)
	}
	got, _, screen := typeAt(t, "pri\t(1)\r"+"pu\t(xs, 1)\r"+"zz\t\r"+"p\t\r", func(e *Editor) { e.Complete = complete })
	want := []string{"print(1)", "push(xs, 1)", "zz", "p"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("read %q, want %q", got, want)
	}
	// Several candidates are listed
	if !strings.Contains(screen, "print    println") || !strings.Contains(screen, "panic") {
		t.Fatalf("the candidates were not listed:\n%q", screen)
	}
}

// The screen after the keys, as a terminal of some width would show it:
// enough of one to check that what is drawn is what was typed, and where
// the cursor is left.
func render(t *testing.T, written string, columns int) (lines []string, row, col int) {
	t.Helper()
	grid := [][]rune{{}}
	put := func(r rune) {
		if col == columns {
			row, col = row+1, 0
		}
		for row >= len(grid) {
			grid = append(grid, []rune{})
		}
		for len(grid[row]) <= col {
			grid[row] = append(grid[row], ' ')
		}
		grid[row][col] = r
		col++
	}
	runes := []rune(written)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '\r':
			col = 0
		case '\n':
			row++
			for row >= len(grid) {
				grid = append(grid, []rune{})
			}
		case '\x1b':
			i++ // [
			n := 0
			for i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
				i++
				n = n*10 + int(runes[i]-'0')
			}
			i++
			switch runes[i] {
			case 'A':
				row -= n
			case 'C':
				col += n
			case 'J':
				grid[row] = grid[row][:min(col, len(grid[row]))]
				grid = grid[:row+1]
			}
		default:
			put(r)
		}
	}
	for _, line := range grid {
		lines = append(lines, strings.TrimRight(string(line), " "))
	}
	return lines, row, col
}

func TestEditorDrawing(t *testing.T) {
	cases := []struct {
		name, keys string
		columns    int
		want       []string
		row, col   int
	}{
		{"one line", "let x = 1", 40, []string{"> let x = 1"}, 0, 11},
		{"cursor moved back", "let x = 1" + left + left, 40, []string{"> let x = 1"}, 0, 9},
		{"several lines", "fn f() {\rreturn 1;", 40, []string{"> fn f() {", ".     return 1;"}, 1, 15},
		{"up into the first line", "fn f() {\rreturn 1;" + up, 40, []string{"> fn f() {", ".     return 1;"}, 0, 10},
		{"a line longer than the terminal", "abcdefghijklmnopqrst", 10, []string{"> abcdefgh", "ijklmnopqr", "st"}, 2, 2},
		{"wrapped, cursor on the first row", "abcdefghijklmnopqrst" + home + right, 10, []string{"> abcdefgh", "ijklmnopqr", "st"}, 0, 3},
		{"exactly to the edge", "abcdefgh", 10, []string{"> abcdefgh", ""}, 1, 0},
		{"edited after wrapping", "abcdefghijkl" + backspace + backspace + backspace + backspace + backspace, 10, []string{"> abcdefg"}, 0, 9},
		{"wide characters", "日本語", 40, []string{"> 日本語"}, 0, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The keys without the one that ends the input, to see it as
			// it is while being typed
			var screen bytes.Buffer
			e := NewEditor(strings.NewReader(c.keys), &screen)
			e.Incomplete = Incomplete
			e.Width = func() int { return c.columns }
			e.begin("> ", ". ")
			for {
				k, err := e.readKey()
				if err != nil {
					break
				}
				e.handle(k)
				e.draw()
			}

			lines, row, col := render(t, screen.String(), c.columns)
			if strings.Join(lines, "\n") != strings.Join(c.want, "\n") {
				t.Fatalf("screen:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(c.want, "\n"))
			}
			if row != c.row || col != c.col {
				t.Fatalf("cursor at row %d column %d, want %d and %d", row, col, c.row, c.col)
			}
		})
	}
}
