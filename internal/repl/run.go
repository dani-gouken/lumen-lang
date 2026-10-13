package repl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/x/term"

	"github.com/dani-gouken/lumen/internal/vm"
)

// interruptedStatus is what a process stopped by Ctrl-C ends with.
const interruptedStatus = 130

// lineTracker passes on what is written and remembers whether it ended a
// line: a program that printed "abc" without one must not have the
// prompt written right after it.
type lineTracker struct {
	to      io.Writer
	midLine bool
}

func (w *lineTracker) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.midLine = p[len(p)-1] != '\n'
	}
	return w.to.Write(p)
}

// Run is `lumen repl`: it reads inputs until there are no more, and
// returns the status the process ends with.
//
// At a terminal, inputs are typed at a prompt, with a history and
// completion. Fed from a file or another program, the same inputs are
// read without any of it, which is what makes a session something that
// can be written down and run again:
//
//	lumen repl < session.txt
func Run(opts Options) int {
	if term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) {
		return runTerminal(opts)
	}
	return runPiped(opts, os.Stdin)
}

// watchInterrupts makes Ctrl-C stop the program that runs, if one does,
// and not the REPL. Without a program running it ends a session that is
// not at a terminal; at one, the editor sees the key itself.
func watchInterrupts(s *Session, running *atomic.Bool, leave bool) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	go func() {
		for range signals {
			if running.Load() {
				s.Machine().Interrupt()
			} else if leave {
				os.Exit(interruptedStatus)
			}
		}
	}()
}

// evaluate takes one input, a command or Lumen.
func evaluate(s *Session, input string, running *atomic.Bool) (quit bool, outcome Outcome) {
	if IsCommand(input) {
		return s.Command(input)
	}
	running.Store(true)
	defer running.Store(false)
	return false, s.Eval(input)
}

// runPiped reads inputs from something that is not a terminal. An input
// ends with its line, unless a bracket is still open at the end of it.
// The status says whether all of them went well.
func runPiped(opts Options, in io.Reader) int {
	reader := bufio.NewReader(in)
	if opts.In == nil {
		// Programs read their input from what is left of ours
		opts.In = reader
	}
	s := New(opts)
	var running atomic.Bool
	watchInterrupts(s, &running, true)

	status := 0
	pending := ""
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			pending += line
			if !Incomplete(pending) || IsCommand(pending) {
				quit, outcome := evaluate(s, pending, &running)
				pending = ""
				if outcome.Exit != nil {
					return *outcome.Exit & 0xFF
				}
				if outcome.Failed {
					status = 1
				}
				if quit {
					return status
				}
			}
		}
		if err != nil {
			break
		}
	}
	if strings.TrimSpace(pending) != "" {
		_, outcome := evaluate(s, pending, &running)
		if outcome.Exit != nil {
			return *outcome.Exit & 0xFF
		}
		if outcome.Failed {
			status = 1
		}
	}
	return status
}

// Colours of the prompt and of what the REPL says itself.
const (
	colourPrompt = "\x1b[36m"
	colourDim    = "\x1b[2m"
	colourReset  = "\x1b[0m"
)

// runTerminal is the REPL someone types at.
func runTerminal(opts Options) int {
	out := &lineTracker{to: os.Stdout}
	if opts.Out == nil {
		opts.Out = out
	}
	s := New(opts)
	var running atomic.Bool
	watchInterrupts(s, &running, false)

	colour := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	paint := func(code, text string) string {
		if !colour {
			return text
		}
		return code + text + colourReset
	}

	editor := NewEditor(os.Stdin, os.Stdout)
	editor.Incomplete = Incomplete
	editor.Complete = s.Complete
	editor.Width = func() int {
		if width, _, err := term.GetSize(os.Stdout.Fd()); err == nil && width > 0 {
			return width
		}
		return 80
	}
	history := historyFile()
	editor.History = readHistory(history)

	fmt.Fprintln(out, "Lumen. "+paint(colourDim, ":help says what can be typed here, Ctrl-D leaves."))
	for {
		if out.midLine {
			fmt.Fprintln(out)
		}
		number := fmt.Sprintf("[%d]", s.Number())
		prompt := paint(colourPrompt, number+">") + " "
		more := paint(colourPrompt, strings.Repeat(".", len(number))+">") + " "

		// Keys arrive one by one while an input is typed, and the way
		// they always do while a program runs: it may read lines itself,
		// and Ctrl-C has to reach it as the signal it is
		state, err := term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot read keys from this terminal: %v\n", err)
			return 1
		}
		input, err := editor.ReadInput(prompt, more)
		term.Restore(os.Stdin.Fd(), state)

		if err == ErrInterrupted {
			continue
		}
		if err != nil {
			return 0
		}
		if strings.TrimSpace(input) == "" {
			continue
		}
		editor.Remember(input)
		appendHistory(history, input)

		quit, outcome := evaluate(s, input, &running)
		if outcome.Exit != nil {
			return *outcome.Exit & 0xFF
		}
		if quit {
			return 0
		}
	}
}

// --- The history, kept from one session to the next ---

// historyLimit is how many inputs are read back.
const historyLimit = 1000

func historyFile() string {
	if path := os.Getenv("LUMEN_HISTORY"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".lumen_history")
}

// An input is one line of the file: the line ends inside it are written
// as \n, and a backslash as two.
var (
	historyEscape   = strings.NewReplacer("\\", "\\\\", "\n", "\\n")
	historyUnescape = strings.NewReplacer("\\\\", "\\", "\\n", "\n")
)

func readHistory(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var inputs []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			inputs = append(inputs, historyUnescape.Replace(line))
		}
	}
	if len(inputs) > historyLimit {
		inputs = inputs[len(inputs)-historyLimit:]
	}
	return inputs
}

func appendHistory(path, input string) {
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintln(file, historyEscape.Replace(strings.TrimRight(input, " \t\r\n")))
}

// NewMachine makes the virtual machine of a session with the limits of
// the command line: how much memory, how deep calls may nest. Zero keeps
// the default of each.
func NewMachine(heapMB, maxDepth int) func() *vm.VM {
	return func() *vm.VM {
		var machine *vm.VM
		if heapMB > 0 {
			machine = vm.NewVMWithHeapSize(uint64(heapMB) * 1024 * 1024)
		} else {
			machine = vm.NewVM()
		}
		if maxDepth > 0 {
			machine.SetMaxCallDepth(maxDepth)
		}
		return machine
	}
}
