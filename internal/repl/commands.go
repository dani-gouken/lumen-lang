package repl

import (
	"fmt"
	"strings"

	"github.com/dani-gouken/lumen/internal/stdlib"
)

// Commands are what is typed to the REPL itself rather than to Lumen.
// They start with a colon, which starts nothing in the language.

type command struct {
	name, short string
	args        string
	help        string
}

var commands = []command{
	{name: ":help", short: ":h", help: "This list"},
	{name: ":vars", short: ":v", help: "The variables, with their types and values"},
	{name: ":decls", short: ":d", help: "What is declared: functions, types, constants, imports"},
	{name: ":type", short: ":t", args: "<expression>", help: "The type of an expression, without running it"},
	{name: ":load", short: ":l", args: "<file.lum>", help: "Take the declarations of a file (its main is left out)"},
	{name: ":forget", short: ":f", args: "<name>", help: "Forget a variable or a declaration"},
	{name: ":reset", help: "Forget everything and start again"},
	{name: ":quit", short: ":q", help: "Leave (also Ctrl-D)"},
}

func commandNames() []string {
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.name)
	}
	return names
}

func standardModules() []string {
	var names []string
	for _, module := range stdlib.Modules() {
		names = append(names, strings.TrimPrefix(module, stdlib.Package+"."))
	}
	return names
}

// IsCommand reports whether an input is for the REPL itself.
func IsCommand(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), ":")
}

// Command runs a command, and reports whether it asks to leave.
func (s *Session) Command(input string) (quit bool, outcome Outcome) {
	name, rest, _ := strings.Cut(strings.TrimSpace(input), " ")
	rest = strings.TrimSpace(rest)
	out := s.opts.Out

	var found *command
	for i, c := range commands {
		if name == c.name || (c.short != "" && name == c.short) {
			found = &commands[i]
		}
	}
	if found == nil {
		s.note("Error: there is no command %s; :help lists them", name)
		return false, Outcome{Failed: true}
	}
	if found.args != "" && rest == "" {
		s.note("Usage: %s %s", found.name, found.args)
		return false, Outcome{Failed: true}
	}

	switch found.name {
	case ":help":
		fmt.Fprintln(out, "Type a statement or an expression to run it, a declaration to keep it.")
		fmt.Fprintln(out, "A declaration typed again replaces the one before. An input goes on to")
		fmt.Fprintln(out, "the next line while a bracket is open.")
		fmt.Fprintln(out)
		for _, c := range commands {
			usage := c.name
			if c.args != "" {
				usage += " " + c.args
			}
			fmt.Fprintf(out, "  %-22s %s\n", usage, c.help)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Keys: Tab completes, Up and Down go through earlier inputs, Ctrl-C")
		fmt.Fprintln(out, "abandons an input or stops a program that runs, Alt-Enter starts a line.")

	case ":vars":
		vars := s.Variables()
		if len(vars) == 0 {
			fmt.Fprintln(out, "No variables yet: let name = value;")
		}
		for _, v := range vars {
			fmt.Fprintf(out, "%s: %s = %s\n", v.Name, v.Type, v.Value)
		}

	case ":decls":
		decls := s.Declarations()
		if len(decls) == 0 {
			fmt.Fprintln(out, "Nothing declared yet.")
		}
		for _, head := range decls {
			fmt.Fprintln(out, head)
		}

	case ":type":
		text, ok := s.TypeOf(rest)
		if !ok {
			return false, Outcome{Failed: true}
		}
		fmt.Fprintln(out, text)

	case ":load":
		return false, s.Load(rest)

	case ":forget":
		found, gone := s.Forget(rest)
		if !found {
			s.note("Error: nothing is called '%s' here", rest)
		}
		if !gone {
			return false, Outcome{Failed: true}
		}

	case ":reset":
		s.Reset()

	case ":quit":
		return true, Outcome{}
	}
	return false, Outcome{}
}
