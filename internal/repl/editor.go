package repl

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// The line editor: what is typed at the prompt, until it is entered.
//
// An input may take several lines, a function for one. They are edited
// together: the arrows go from one to the other, and an input called
// back from the history comes back whole. Enter ends the input when it
// reads as complete, and starts a new line when a bracket is still open.
//
// The editor reads keys and writes escape sequences; putting the
// terminal in the mode where keys arrive one by one is left to whoever
// calls it (see Run), which is what lets tests drive it with plain bytes.

// ErrInterrupted is returned by ReadInput when the input was abandoned
// with Ctrl-C.
var ErrInterrupted = errors.New("interrupted")

// Editor reads inputs from a terminal.
type Editor struct {
	in  io.Reader
	out io.Writer

	// Width gives the number of columns of the terminal.
	Width func() int

	// Incomplete reports whether a text needs more lines before it can
	// be entered.
	Incomplete func(text string) bool

	// Complete gives what could be meant by the text before the cursor:
	// the candidates, and how many of the characters before the cursor
	// they replace.
	Complete func(before string) (candidates []string, replaced int)

	// History holds the earlier inputs, oldest first.
	History []string

	pending []byte // Read from the terminal, not looked at yet

	// The input being edited
	buf    []rune
	pos    int
	prompt string
	more   string // The prompt of the lines after the first
	row    int    // The row the cursor was left on, counted from the first of the input
	at     int    // Which input of the history is shown; len(History) for the new one
	draft  []rune // The new input, while an earlier one is shown
}

// NewEditor makes an editor that reads keys from in and draws on out.
func NewEditor(in io.Reader, out io.Writer) *Editor {
	return &Editor{in: in, out: out, Width: func() int { return 80 }}
}

// Keys that are not characters.
const (
	keyRune = iota
	keyEnter
	keyNewline
	keyInterrupt
	keyEnd // Ctrl-D
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyHome
	keyLineEnd
	keyWordLeft
	keyWordRight
	keyKillLine
	keyKillStart
	keyKillWord
	keyClear
	keyTab
	keyIgnored
)

type key struct {
	code int
	r    rune
}

func (e *Editor) readByte() (byte, error) {
	if len(e.pending) == 0 {
		chunk := make([]byte, 256)
		n, err := e.in.Read(chunk)
		if n == 0 {
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
		e.pending = chunk[:n]
	}
	b := e.pending[0]
	e.pending = e.pending[1:]
	return b, nil
}

// readKey reads one key: a character, or what an escape sequence means.
func (e *Editor) readKey() (key, error) {
	b, err := e.readByte()
	if err != nil {
		return key{}, err
	}
	switch b {
	case '\r', '\n':
		return key{code: keyEnter}, nil
	case 1:
		return key{code: keyHome}, nil
	case 2:
		return key{code: keyLeft}, nil
	case 3:
		return key{code: keyInterrupt}, nil
	case 4:
		return key{code: keyEnd}, nil
	case 5:
		return key{code: keyLineEnd}, nil
	case 6:
		return key{code: keyRight}, nil
	case 8, 127:
		return key{code: keyBackspace}, nil
	case '\t':
		return key{code: keyTab}, nil
	case 11:
		return key{code: keyKillLine}, nil
	case 12:
		return key{code: keyClear}, nil
	case 14:
		return key{code: keyDown}, nil
	case 16:
		return key{code: keyUp}, nil
	case 21:
		return key{code: keyKillStart}, nil
	case 23:
		return key{code: keyKillWord}, nil
	case 27:
		return e.readEscape()
	}
	if b < 32 {
		return key{code: keyIgnored}, nil
	}

	// A character, of one byte or several
	encoded := []byte{b}
	for !utf8.FullRune(encoded) && len(encoded) < utf8.UTFMax {
		next, err := e.readByte()
		if err != nil {
			return key{}, err
		}
		encoded = append(encoded, next)
	}
	r, _ := utf8.DecodeRune(encoded)
	if r == utf8.RuneError {
		return key{code: keyIgnored}, nil
	}
	return key{code: keyRune, r: r}, nil
}

// readEscape reads what follows an escape character.
func (e *Editor) readEscape() (key, error) {
	b, err := e.readByte()
	if err != nil {
		return key{}, err
	}
	switch b {
	case '\r', '\n':
		return key{code: keyNewline}, nil // Alt-Enter
	case 'b':
		return key{code: keyWordLeft}, nil
	case 'f':
		return key{code: keyWordRight}, nil
	case 127:
		return key{code: keyKillWord}, nil
	case 'O':
		b, err = e.readByte()
		if err != nil {
			return key{}, err
		}
		return key{code: finalKey(b, "")}, nil
	case '[':
		var params []byte
		for {
			b, err = e.readByte()
			if err != nil {
				return key{}, err
			}
			if b >= 0x40 && b <= 0x7e {
				break
			}
			params = append(params, b)
		}
		return key{code: finalKey(b, string(params))}, nil
	}
	return key{code: keyIgnored}, nil
}

// finalKey gives the key of an escape sequence, by its last character
// and what stood before it.
func finalKey(final byte, params string) int {
	modified := strings.Contains(params, ";") // With Ctrl or Alt held
	switch final {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	case 'C':
		if modified {
			return keyWordRight
		}
		return keyRight
	case 'D':
		if modified {
			return keyWordLeft
		}
		return keyLeft
	case 'H':
		return keyHome
	case 'F':
		return keyLineEnd
	case '~':
		switch params {
		case "1", "7":
			return keyHome
		case "4", "8":
			return keyLineEnd
		case "3":
			return keyDelete
		}
	}
	return keyIgnored
}

// ReadInput shows a prompt and reads one input. more is the prompt of
// its lines after the first. It returns io.EOF when the terminal is
// closed, or Ctrl-D is typed on an empty input, and ErrInterrupted after
// Ctrl-C.
func (e *Editor) ReadInput(prompt, more string) (string, error) {
	e.begin(prompt, more)
	for {
		k, err := e.readKey()
		if err != nil {
			e.finish()
			if len(e.buf) > 0 && err == io.EOF {
				return string(e.buf), nil
			}
			return "", err
		}
		if text, done, err := e.handle(k); done {
			return text, err
		}
		e.draw()
	}
}

// begin starts a new input, and shows its prompt.
func (e *Editor) begin(prompt, more string) {
	e.buf, e.pos, e.row = e.buf[:0], 0, 0
	e.prompt, e.more = prompt, more
	e.at, e.draft = len(e.History), nil
	e.draw()
}

// handle does what a key asks for, and reports whether it ended the
// input: entered, abandoned, or the terminal left.
func (e *Editor) handle(k key) (text string, done bool, err error) {
	switch k.code {
	case keyRune:
		e.typed(k.r)
	case keyEnter:
		text := string(e.buf)
		if e.Incomplete != nil && strings.TrimSpace(text) != "" && e.Incomplete(text) {
			e.newline()
			break
		}
		e.finish()
		return text, true, nil
	case keyNewline:
		e.newline()
	case keyInterrupt:
		e.finish()
		return "", true, ErrInterrupted
	case keyEnd:
		if len(e.buf) == 0 {
			e.finish()
			return "", true, io.EOF
		}
		e.deleteRange(e.pos, e.pos+1)
	case keyBackspace:
		e.backspace()
	case keyDelete:
		e.deleteRange(e.pos, e.pos+1)
	case keyLeft:
		e.moveTo(e.pos - 1)
	case keyRight:
		e.moveTo(e.pos + 1)
	case keyHome:
		e.moveTo(e.lineStart(e.pos))
	case keyLineEnd:
		e.moveTo(e.lineEnd(e.pos))
	case keyWordLeft:
		e.moveTo(e.wordLeft())
	case keyWordRight:
		e.moveTo(e.wordRight())
	case keyUp:
		e.vertical(-1)
	case keyDown:
		e.vertical(1)
	case keyKillLine:
		e.deleteRange(e.pos, e.lineEnd(e.pos))
	case keyKillStart:
		e.deleteRange(e.lineStart(e.pos), e.pos)
	case keyKillWord:
		e.deleteRange(e.wordLeft(), e.pos)
	case keyClear:
		fmt.Fprint(e.out, "\x1b[H\x1b[2J")
		e.row = 0
	case keyTab:
		e.tab()
	}
	return "", false, nil
}

// Remember adds an input to the history, unless it is the one before.
func (e *Editor) Remember(input string) {
	input = strings.TrimRight(input, " \t\r\n")
	if strings.TrimSpace(input) == "" {
		return
	}
	if n := len(e.History); n > 0 && e.History[n-1] == input {
		return
	}
	e.History = append(e.History, input)
}

// --- Editing ---

func (e *Editor) insert(text []rune) {
	e.buf = append(e.buf[:e.pos], append(append([]rune(nil), text...), e.buf[e.pos:]...)...)
	e.pos += len(text)
}

func (e *Editor) deleteRange(from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(e.buf) {
		to = len(e.buf)
	}
	if from >= to {
		return
	}
	e.buf = append(e.buf[:from], e.buf[to:]...)
	e.pos = from
}

func (e *Editor) moveTo(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(e.buf) {
		pos = len(e.buf)
	}
	e.pos = pos
}

// indentWidth is how far a line inside a block is from the block's.
const indentWidth = 4

// typed takes a character. A closing brace that is the first thing on
// its line goes back to where the block it closes started.
func (e *Editor) typed(r rune) {
	if r == '}' {
		start := e.lineStart(e.pos)
		if before := string(e.buf[start:e.pos]); before != "" && strings.TrimSpace(before) == "" {
			back := indentWidth
			if back > len(before) {
				back = len(before)
			}
			e.deleteRange(e.pos-back, e.pos)
		}
	}
	e.insert([]rune{r})
}

// backspace deletes the character before the cursor, or a level of
// indentation when there is nothing else before it on the line.
func (e *Editor) backspace() {
	start := e.lineStart(e.pos)
	before := string(e.buf[start:e.pos])
	if n := len(before); n >= indentWidth && strings.TrimSpace(before) == "" && n%indentWidth == 0 && !strings.Contains(before, "\t") {
		e.deleteRange(e.pos-indentWidth, e.pos)
		return
	}
	e.deleteRange(e.pos-1, e.pos)
}

// newline starts a new line, as far in as the one it is typed on, and
// further in when that one leaves a bracket open.
func (e *Editor) newline() {
	start := e.lineStart(e.pos)
	line := string(e.buf[start:e.pos])
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	if strings.Count(line, "{")+strings.Count(line, "(")+strings.Count(line, "[") >
		strings.Count(line, "}")+strings.Count(line, ")")+strings.Count(line, "]") {
		indent += strings.Repeat(" ", indentWidth)
	}
	e.insert([]rune("\n" + indent))
}

func (e *Editor) lineStart(pos int) int {
	for pos > 0 && e.buf[pos-1] != '\n' {
		pos--
	}
	return pos
}

func (e *Editor) lineEnd(pos int) int {
	for pos < len(e.buf) && e.buf[pos] != '\n' {
		pos++
	}
	return pos
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (e *Editor) wordLeft() int {
	pos := e.pos
	for pos > 0 && !isWordRune(e.buf[pos-1]) {
		pos--
	}
	for pos > 0 && isWordRune(e.buf[pos-1]) {
		pos--
	}
	return pos
}

func (e *Editor) wordRight() int {
	pos := e.pos
	for pos < len(e.buf) && !isWordRune(e.buf[pos]) {
		pos++
	}
	for pos < len(e.buf) && isWordRune(e.buf[pos]) {
		pos++
	}
	return pos
}

// vertical moves the cursor a line up or down inside the input, and
// past its first or last line to the input before or after it in the
// history.
func (e *Editor) vertical(by int) {
	start := e.lineStart(e.pos)
	column := e.pos - start
	if by < 0 && start > 0 {
		previous := e.lineStart(start - 1)
		e.pos = previous + min(column, start-1-previous)
		return
	}
	if end := e.lineEnd(e.pos); by > 0 && end < len(e.buf) {
		next := end + 1
		e.pos = next + min(column, e.lineEnd(next)-next)
		return
	}

	to := e.at + by
	if to < 0 || to > len(e.History) {
		return
	}
	if e.at == len(e.History) {
		e.draft = append([]rune(nil), e.buf...)
	}
	e.at = to
	if to == len(e.History) {
		e.buf = append(e.buf[:0], e.draft...)
	} else {
		e.buf = append(e.buf[:0], []rune(e.History[to])...)
	}
	e.pos = len(e.buf)
}

// tab completes the name before the cursor. At the start of a line,
// where there is nothing to complete, it indents.
func (e *Editor) tab() {
	start := e.lineStart(e.pos)
	if strings.TrimSpace(string(e.buf[start:e.pos])) == "" {
		e.insert([]rune(strings.Repeat(" ", indentWidth-(e.pos-start)%indentWidth)))
		return
	}
	if e.Complete == nil {
		return
	}
	candidates, replaced := e.Complete(string(e.buf[:e.pos]))
	if len(candidates) == 0 || replaced > e.pos {
		return
	}

	// As far as the candidates agree
	common := []rune(candidates[0])
	for _, candidate := range candidates[1:] {
		other := []rune(candidate)
		n := 0
		for n < len(common) && n < len(other) && common[n] == other[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) > replaced {
		e.deleteRange(e.pos-replaced, e.pos)
		e.insert(common)
	}
	if len(candidates) > 1 {
		e.list(candidates)
	}
}

// list writes the candidates under the input, which is then drawn again
// below them.
func (e *Editor) list(candidates []string) {
	saved := e.pos
	e.pos = len(e.buf)
	e.draw()
	e.pos = saved

	width := 0
	for _, candidate := range candidates {
		width = max(width, runewidth.StringWidth(candidate))
	}
	width += 2
	perRow := max(1, e.Width()/width)
	const most = 60
	var sb strings.Builder
	sb.WriteString("\r\n")
	for i, candidate := range candidates {
		if i == most {
			fmt.Fprintf(&sb, "... %d more", len(candidates)-most)
			break
		}
		sb.WriteString(candidate)
		if (i+1)%perRow == 0 && i+1 < len(candidates) {
			sb.WriteString("\r\n")
		} else {
			sb.WriteString(strings.Repeat(" ", width-runewidth.StringWidth(candidate)))
		}
	}
	sb.WriteString("\r\n")
	fmt.Fprint(e.out, sb.String())
	e.row = 0
}

// --- Drawing ---

// visibleWidth is the number of columns a text takes, escape sequences
// that colour it left out.
func visibleWidth(text string) int {
	width, escape := 0, false
	for _, r := range text {
		switch {
		case escape:
			if r == 'm' {
				escape = false
			}
		case r == '\x1b':
			escape = true
		default:
			width += runewidth.RuneWidth(r)
		}
	}
	return width
}

// draw writes the input again from its first row, and puts the cursor
// where it is in the text.
func (e *Editor) draw() {
	columns := e.Width()
	if columns < 8 {
		columns = 8
	}
	var sb strings.Builder
	if e.row > 0 {
		fmt.Fprintf(&sb, "\x1b[%dA", e.row)
	}
	sb.WriteString("\r\x1b[J")

	lines := strings.Split(string(e.buf), "\n")
	cursorLine := strings.Count(string(e.buf[:e.pos]), "\n")
	cursorColumn := e.pos - e.lineStart(e.pos)

	rows, cursorRow, cursorAt := 0, 0, 0
	for i, line := range lines {
		prompt := e.prompt
		if i > 0 {
			prompt = e.more
		}
		sb.WriteString(prompt)
		sb.WriteString(line)
		width := visibleWidth(prompt) + runewidth.StringWidth(line)

		if i == cursorLine {
			before := visibleWidth(prompt) + runewidth.StringWidth(string([]rune(line)[:cursorColumn]))
			cursorRow, cursorAt = rows+before/columns, before%columns
		}

		// A line that fills its last row to the edge leaves the cursor
		// waiting there: the next row exists once something is written
		taken := 1
		if width > 0 {
			taken = (width + columns - 1) / columns
		}
		if i == len(lines)-1 {
			if width > 0 && width%columns == 0 {
				sb.WriteString("\r\n")
				taken++
			}
		} else {
			sb.WriteString("\r\n")
		}
		rows += taken
	}

	if up := rows - 1 - cursorRow; up > 0 {
		fmt.Fprintf(&sb, "\x1b[%dA", up)
	}
	sb.WriteString("\r")
	if cursorAt > 0 {
		fmt.Fprintf(&sb, "\x1b[%dC", cursorAt)
	}
	e.row = cursorRow
	fmt.Fprint(e.out, sb.String())
}

// finish leaves the input as it is on the screen, the cursor on the line
// after it.
func (e *Editor) finish() {
	e.pos = len(e.buf)
	e.draw()
	fmt.Fprint(e.out, "\r\n")
	e.row = 0
}
