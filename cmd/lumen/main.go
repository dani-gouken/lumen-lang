package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"runtime/pprof"

	lumen "github.com/dani-gouken/lumen"
	"github.com/dani-gouken/lumen/internal/asm"
	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/debugger"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/formatter"
	_ "github.com/dani-gouken/lumen/internal/inspect" // The natives of std.compiler
	"github.com/dani-gouken/lumen/internal/lsp"
	"github.com/dani-gouken/lumen/internal/native"
	"github.com/dani-gouken/lumen/internal/pkgmgr"
	"github.com/dani-gouken/lumen/internal/project"
	"github.com/dani-gouken/lumen/internal/repl"
	"github.com/dani-gouken/lumen/internal/runner"
	"github.com/dani-gouken/lumen/internal/testrun"
	"github.com/dani-gouken/lumen/internal/vm"
)

// version is what lumen version says: set when a release is built
// (-ldflags "-X main.version=v1.2.3"), "dev" otherwise.
var version = "dev"

// Limits of the virtual machine, settable from the command line and from
// the project file; the command line wins.
var (
	strip    = false // Leave debug information out of what build writes
	heapMB   = 0     // 0: the VM's default
	maxDepth = 0
)

// Where --cpuprofile writes what the processor was doing, for go tool pprof
var cpuProfile = ""

// showResult, set by --result, makes run print what main returned instead
// of making it the exit status: how a program written in assembly is looked
// at, whose main may return anything
var showResult = false

// startProfile begins recording for --cpuprofile, if it was given.
func startProfile() {
	if cpuProfile == "" {
		return
	}
	file, err := os.Create(cpuProfile)
	if err != nil {
		fatal(err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		fatal(err)
	}
}

// exit ends the process, with the profile written out first: os.Exit
// does not wait for anything.
func exit(code int) {
	if cpuProfile != "" {
		pprof.StopCPUProfile()
	}
	os.Exit(code)
}

// fatal prints an error and ends the process.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	exit(1)
}

// sourceFile returns the .lum file a command should compile. arg is what
// was given on the command line: a file, a project folder, or nothing, in
// which case the project of the current directory is used. usage is
// printed when there is nothing to go by.
func sourceFile(arg, usage string) string {
	dir := "."
	if arg != "" {
		info, err := os.Stat(arg)
		if err != nil || !info.IsDir() {
			return arg // A file; a missing one is reported when it is read
		}
		dir = arg
	}

	cfg, err := project.Find(dir)
	if err != nil {
		fatal(err)
	}
	if cfg == nil {
		if arg == "" {
			fmt.Println(usage)
			exit(1)
		}
		fatal(fmt.Errorf("%s is a folder without a %s; name a .lum file, or add a project file with an \"entry\"", arg, project.FileName))
	}
	entry, err := cfg.EntryFile()
	if err != nil {
		fatal(err)
	}
	return entry
}

// projectOptions returns where the modules of a source file are found,
// from the project file in its folder or above, and applies the limits the
// project sets unless the command line already did.
func projectOptions(inputFile string) compiler.Options {
	cfg, err := project.Find(filepath.Dir(inputFile))
	if err != nil {
		fatal(err)
	}
	if cfg == nil {
		return compiler.Options{}
	}
	if heapMB == 0 {
		heapMB = cfg.HeapMB
	}
	if maxDepth == 0 {
		maxDepth = cfg.MaxDepth
	}
	if err := cfg.CheckInstalled(); err != nil {
		fatal(err)
	}
	return compiler.Options{Root: cfg.RootDir(), Packages: cfg.PackageDirs()}
}

// argument returns the i-th command line argument, or "".
func argument(i int) string {
	if i < len(os.Args) {
		return os.Args[i]
	}
	return ""
}

// parseOptions removes the --name=value options from the arguments and
// applies them. They may appear anywhere after the command.
//
// What follows the file to run is for the program (std.os.args), except
// the options of lumen itself; after a lone "--" everything is for the
// program, whatever it looks like.
func parseOptions(args []string) []string {
	var rest []string
	for i, arg := range args {
		name, value, isOption := strings.Cut(arg, "=")
		afterFile := len(rest) >= 3
		if arg == "--" && afterFile {
			return append(rest, args[i+1:]...)
		}
		isOurs := name == "--heap-mb" || name == "--max-depth" || name == "--cpuprofile" || arg == "--strip" || arg == "--result"
		// fmt, test, check and dump have options of their own, which they read themselves
		ofTool := len(rest) >= 2 && (rest[1] == "fmt" || rest[1] == "test" || rest[1] == "check" || rest[1] == "dump" || slices.Contains(pkgmgr.Commands, rest[1]))
		if !strings.HasPrefix(arg, "--") || ((afterFile || ofTool) && !isOurs) {
			rest = append(rest, arg)
			continue
		}
		if arg == "--strip" {
			strip = true
			continue
		}
		if arg == "--result" {
			showResult = true
			continue
		}
		if name == "--cpuprofile" {
			if !isOption || value == "" {
				fmt.Fprintf(os.Stderr, "Error: option --cpuprofile needs a file, as in --cpuprofile=cpu.prof\n")
				exit(1)
			}
			cpuProfile = value
			continue
		}

		n, err := strconv.Atoi(value)
		if !isOption || err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "Error: option %s needs a positive number, as in %s=64\n", name, name)
			exit(1)
		}
		switch name {
		case "--heap-mb":
			heapMB = n
		case "--max-depth":
			maxDepth = n
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n", name)
			exit(1)
		}
	}
	return rest
}

// A program runs on the thread lumen started on, and stays there: a
// library that opens a window wants every call about it made from the main
// thread of the process (macOS allows no other), and a function of a C
// library is called from the thread the program is running on.
func init() {
	runtime.LockOSThread()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Args[1] = "version"
	}
	if len(os.Args) > 1 && isProgram(os.Args[1]) {
		// lumen script.lum is lumen run script.lum: what a file that starts
		// with #!/usr/bin/env lumen is run with
		os.Args = append([]string{os.Args[0], "run"}, os.Args[1:]...)
	}
	os.Args = parseOptions(os.Args)
	startProfile()
	defer pprof.StopCPUProfile()
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	command := os.Args[1]

	switch command {
	case "run":
		cmdRun(sourceFile(argument(2), "Usage: lumen run [file.lum | file.lc | file.lasm | project folder] [arguments]"))

	case "compile":
		// With no file, the project's entry is compiled
		args, output := outputOption(os.Args[2:])
		if len(args) > 1 {
			fmt.Println("Usage: lumen compile [file.lum | project folder] [-o file.lc]")
			exit(1)
		}
		input := sourceFile(firstOf(args), "Usage: lumen compile [file.lum | project folder] [-o file.lc]")
		cmdCompile(input, bytecodeOutput(input, output))

	case "assemble":
		args, output := outputOption(os.Args[2:])
		if len(args) != 1 {
			fmt.Println("Usage: lumen assemble <file.lasm> [-o file.lc]")
			exit(1)
		}
		cmdAssemble(args[0], bytecodeOutput(args[0], output))

	case "dump":
		cmdDump(os.Args[2:])

	case "native":
		args, output := outputOption(os.Args[2:])
		if len(args) > 1 {
			fmt.Println("Usage: lumen native [file.lum | project folder] [-o executable]")
			exit(1)
		}
		cmdNative(sourceFile(firstOf(args), "Usage: lumen native [file.lum | project folder] [-o executable]"), output)

	case "debug":
		cmdDebug(sourceFile(argument(2), "Usage: lumen debug [file.lum | file.lc | file.lasm | project folder]"))

	case "repl":
		cmdRepl()

	case "lsp":
		cmdLsp()

	case "fmt":
		cmdFmt()

	case "test":
		cmdTest(os.Args[2:])

	case "check":
		cmdCheck(os.Args[2:])

	case "version":
		// The machine as it calls itself (uname -m), as the releases are named
		arch := runtime.GOARCH
		if arch == "amd64" {
			arch = "x86_64"
		}
		fmt.Printf("lumen %s %s/%s\n", version, runtime.GOOS, arch)

	case "install", "update", "require", "remove", "show", "why", "outdated":
		cmdPackages()

	default:
		if now, renamed := renamedCommands[command]; renamed {
			fmt.Fprintf(os.Stderr, "Error: lumen %s is now lumen %s\n", command, now)
			exit(1)
		}
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		exit(1)
	}
}

// renamedCommands are the commands that have another name now: one verb
// for each thing to do, the file saying the rest.
var renamedCommands = map[string]string{
	"build":  "compile (bytecode) or lumen dump (a listing)",
	"ir":     "dump --ir",
	"asm":    "assemble",
	"disasm": "dump",
	"exec":   "run",
	"runasm": "run",
	"lumdbg": "debug",
}

// outputOption takes "-o file" out of the arguments of a command that
// writes a file, and returns the rest and the file ("" when not given).
func outputOption(args []string) ([]string, string) {
	var rest []string
	output := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			if i+1 >= len(args) {
				fatal(fmt.Errorf("-o needs a file, as in -o app.lc"))
			}
			output = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return rest, output
}

// firstOf is the first of the arguments, or "".
func firstOf(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// bytecodeOutput is where a command that makes bytecode writes it: the
// file asked for, which has to be a .lc, or the input's name with .lc in
// the current folder.
func bytecodeOutput(input, output string) string {
	if output == "" {
		name := filepath.Base(input)
		return strings.TrimSuffix(name, filepath.Ext(name)) + ".lc"
	}
	if filepath.Ext(output) != ".lc" {
		fatal(fmt.Errorf("%s: bytecode is written to a .lc file; lumen dump prints a listing", output))
	}
	return output
}

// loadProgram makes the chunk of a program to run, debug or look at:
// compiled from source, read from bytecode, or assembled.
func loadProgram(inputFile string) *vm.Chunk {
	programFile = inputFile
	switch filepath.Ext(inputFile) {
	case ".lc":
		data, err := os.ReadFile(inputFile)
		if err != nil {
			fatal(err)
		}
		chunk, err := vm.DeserializeChunk(data)
		if err != nil {
			fatal(fmt.Errorf("%s: %v", inputFile, err))
		}
		return chunk
	case ".lasm":
		data, err := os.ReadFile(inputFile)
		if err != nil {
			fatal(err)
		}
		chunk, err := asm.AssembleFromFile(string(data), inputFile)
		if err != nil {
			fatal(fmt.Errorf("%s: %v", inputFile, err))
		}
		return chunk
	}
	return compileSource(inputFile).Chunk
}

// commands are the first words lumen understands. A file of that name is
// run with lumen run.
var commands = append([]string{"run", "compile", "native", "assemble", "dump", "debug", "repl", "lsp", "fmt", "test", "check", "version"}, pkgmgr.Commands...)

// isProgram reports whether a first argument names a program to run
// rather than a command: a .lum, .lc or .lasm file, or any file that is not
// called as a command is (a script without an extension, run through #!).
func isProgram(arg string) bool {
	if strings.HasSuffix(arg, ".lum") || strings.HasSuffix(arg, ".lc") || strings.HasSuffix(arg, ".lasm") {
		return true
	}
	if slices.Contains(commands, arg) || strings.HasPrefix(arg, "-") {
		return false
	}
	info, err := os.Stat(arg)
	return err == nil && info.Mode().IsRegular()
}

func printHelp() {
	fmt.Println("Lumen Programming Language")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  lumen run <file> [args]    Run a program: .lum source, .lc bytecode or .lasm assembly")
	fmt.Println("  lumen <file.lum> [args]    The same as lumen run: what #!/usr/bin/env lumen runs")
	fmt.Println("  lumen compile <file.lum>   Compile to bytecode, file.lc; -o names it, --strip makes it smaller")
	fmt.Println("  lumen native <file.lum>    Compile to an executable, through Go; -o names it")
	fmt.Println("  lumen assemble <file.lasm> Assemble to bytecode, file.lc; -o names it")
	fmt.Println("  lumen dump <file> [--ir]   Print what a .lum or .lc is as bytecode; --ir, the intermediate form")
	fmt.Println("  lumen debug <file>         Step through a .lum, .lc or .lasm program")
	fmt.Println("  lumen repl                 Type Lumen in a line at a time")
	fmt.Println("  lumen test [files]         Run the tests: the functions test...() of the files *_test.lum")
	fmt.Println("  lumen check [--json] [files] Compile without running and say what is wrong; --json for tools")
	fmt.Println("  lumen fmt [files]          Format .lum files where they are; --check only says which would change")
	fmt.Println("  lumen require <vendor/name> Add a package to the project and install it")
	fmt.Println("  lumen install [--no-dev]   Install the packages of lumen.lock (resolve first if there is none)")
	fmt.Println("  lumen update [vendor/name] Resolve the packages again, write lumen.lock, install")
	fmt.Println("  lumen remove <vendor/name> Remove a package from the project")
	fmt.Println("  lumen show [vendor/name]   List the installed packages, or tell all about one")
	fmt.Println("  lumen why <vendor/name>    Say what requires a package")
	fmt.Println("  lumen outdated             List the packages that have newer versions")
	fmt.Println("  lumen version              Say which lumen this is")
	fmt.Println("  lumen lsp                  Start the language server, for an editor to talk to")
	fmt.Println()
	fmt.Println("Options for commands that execute a program:")
	fmt.Println("  --heap-mb=N                Memory the program may use, in MB (default 256)")
	fmt.Println("  --max-depth=N              How deep calls may nest (default 10000)")
	fmt.Println("  --cpuprofile=FILE          Write where the time went, for go tool pprof")
	fmt.Println("  --result                   Print what main returned, instead of making it the exit status")
	fmt.Println()
	fmt.Println("Option for compile:")
	fmt.Println("  --strip                    Leave out what errors use to name the source line:")
	fmt.Println("                             a smaller file, whose errors name the instruction")
	fmt.Println()
	fmt.Println("Projects:")
	fmt.Println("  A lumen.json in the program's folder, or above it, says where the program")
	fmt.Println("  starts (\"entry\"), where module paths start (\"root\"), which packages it")
	fmt.Println("  uses (\"packages\") and its limits (\"heap_mb\", \"max_depth\"). With one,")
	fmt.Println("  run, compile, dump and debug take a project folder, or nothing at all:")
	fmt.Println("    lumen run                  Run the project of the current folder")
	fmt.Println("    lumen run path/to/project  Run the project in that folder")
	fmt.Println("    lumen compile -o app.lc    Compile the project of the current folder")
}

// compileSource runs the compilation pipeline on a .lum file. Diagnostics
// are printed to stderr; the process exits if the program does not compile.
func compileSource(inputFile string) *compiler.Result {
	programFile = inputFile
	errs := errors.NewHandler()
	result, err := compiler.CompileFileOptions(inputFile, projectOptions(inputFile), errs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		exit(1)
	}

	for _, d := range errs.Diagnostics {
		fmt.Fprintln(os.Stderr, d.String())
	}
	if errs.HasErrors() {
		exit(1)
	}
	return result
}

// newVM creates a virtual machine with the limits from the command line
// and the project file, and what follows the file as the program's
// arguments.
func newVM() *vm.VM {
	var args []string
	if len(os.Args) > 3 {
		args = os.Args[3:]
	}
	return runner.Machine(runner.Limits{HeapMB: heapMB, MaxDepth: maxDepth}, args, programFile, exit)
}

// leaveIfAsked ends the process with the status the program asked for, if
// the run ended by a call of std.os.exit and not by a failure. A program
// stopped from the keyboard ends as the shell expects, having said where
// it was.
func leaveIfAsked(err error) {
	runner.LeaveIfAsked(err, exit)
}

// programFile is the file being run, for the program to find what comes
// with it (std.os.program).
var programFile string

// cmdRun runs a program: compiled from source, read from bytecode made
// earlier, or assembled. An Int returned by main becomes the process exit
// code; the run writes nothing of its own.
func cmdRun(inputFile string) {
	chunk := loadProgram(inputFile)

	result, err := newVM().Exec(chunk)
	if err == nil && showResult {
		fmt.Printf("Result: %s\n", formatValue(result))
		return
	}
	runner.Finish(result, err, exit)
}

// cmdRepl runs the REPL. Started inside a project it imports what the
// project's programs can: paths start at the project's root, and its
// packages are there. The limits of the project and of the command line
// apply to what is typed as they do to a program.
func cmdRepl() {
	opts := repl.Options{}
	cfg, err := project.Find(".")
	if err != nil {
		fatal(err)
	}
	if cfg != nil {
		opts.Dir, opts.Packages = cfg.RootDir(), cfg.PackageDirs()
		if heapMB == 0 {
			heapMB = cfg.HeapMB
		}
		if maxDepth == 0 {
			maxDepth = cfg.MaxDepth
		}
	}
	machine := repl.NewMachine(heapMB, maxDepth)
	opts.Machine = func() *vm.VM {
		m := machine()
		runner.GuardMemory(m)
		return m
	}
	exit(repl.Run(opts))
}

// runTool runs a Lumen program kept in the binary, with what follows the
// command as its arguments, and returns the status it ended with: what
// its main returned, or 1 if it failed.
func runTool(source, file string) int {
	return runToolWith(source, file, compiler.Options{}, os.Args[2:])
}

// runToolWith is runTool for a program of several modules, whose files
// opts says how to read, given the arguments it gets.
func runToolWith(source, file string, opts compiler.Options, args []string) int {
	errs := errors.NewHandler()
	result := compiler.CompileOptions(source, file, opts, errs)
	if errs.HasErrors() {
		for _, d := range errs.Diagnostics {
			fmt.Fprintln(os.Stderr, d.String())
		}
		return 1
	}
	programFile = file
	machine := newVM()
	machine.SetArgs(args)
	value, err := machine.Exec(result.Chunk)
	if err != nil {
		leaveIfAsked(err)
		fmt.Fprint(os.Stderr, strings.TrimRight(err.Error(), "\n")+"\n")
		return 1
	}
	if value.IsInt() {
		return int(value.AsInt() & 0xFF)
	}
	return 0
}

// cmdLsp runs the language server: a Lumen program kept in the binary,
// which talks to an editor over the input and the output. It ends when
// the editor says so, by a call of os.exit; coming back from its main
// means the editor went away without a word.
func cmdLsp() {
	runTool(lsp.Source, lsp.File)
	exit(1)
}

// cmdPackages runs the package manager, a Lumen program too, with the
// command as its first argument: lumen install, update, require, remove,
// show.
func cmdPackages() {
	exit(runToolWith(pkgmgr.Source, pkgmgr.File, pkgmgr.Options(), os.Args[1:]))
}

// cmdFmt runs the formatter: a Lumen program too.
func cmdFmt() {
	exit(runTool(formatter.Source, formatter.File))
}

// cmdTest runs the tests of the files and folders named, or of the
// current folder: every function test...() of every file *_test.lum.
// Each file is a program of its own, compiled with a main that runs its
// tests (see internal/testrun). The process ends with 1 if a test failed
// or a file could not be run.
// cmdCheck compiles programs without running them and says what is wrong
// with them: for an editor, a CI step or an agent that writes code. With
// --json the diagnostics are one JSON document on the standard output.
func cmdCheck(args []string) {
	const usage = "Usage: lumen check [--json] [file.lum | project folder]..."
	asJSON := false
	var paths []string
	for _, arg := range args {
		switch {
		case arg == "--json":
			asJSON = true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n%s\n", arg, usage)
			os.Exit(2)
		default:
			paths = append(paths, arg)
		}
	}
	if len(paths) == 0 {
		paths = []string{""}
	}

	type position struct {
		File      string `json:"file"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
		EndColumn int    `json:"endColumn,omitempty"`
	}
	type diagnostic struct {
		Severity string `json:"severity"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		position
		Suggestion string `json:"suggestion,omitempty"`
	}
	report := struct {
		OK          bool         `json:"ok"`
		Files       []string     `json:"files"`
		Diagnostics []diagnostic `json:"diagnostics"`
	}{OK: true, Diagnostics: []diagnostic{}}

	for _, path := range paths {
		file := sourceFile(path, usage)
		report.Files = append(report.Files, file)
		errs := errors.NewHandler()
		if _, err := compiler.CompileFileOptions(file, projectOptions(file), errs); err != nil {
			fatal(err)
		}
		for _, d := range errs.Diagnostics {
			if d.Severity == errors.SeverityError {
				report.OK = false
			}
			if !asJSON {
				fmt.Fprintln(os.Stderr, d.String())
				continue
			}
			at := position{File: d.Pos.File, Line: d.Pos.Line, Column: d.Pos.Col}
			if len(d.Highlights) > 0 {
				at.EndColumn = d.Highlights[0].EndCol
			}
			report.Diagnostics = append(report.Diagnostics, diagnostic{
				Severity: strings.ToLower(d.Severity.String()), Code: d.Code, Message: d.Message,
				position: at, Suggestion: d.Suggestion,
			})
		}
	}

	if asJSON {
		out, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(out))
	} else if report.OK {
		fmt.Printf("%s: no problems\n", strings.Join(report.Files, ", "))
	}
	if !report.OK {
		os.Exit(1)
	}
}

func cmdTest(args []string) {
	const usage = "Usage: lumen test [--filter=text] [file_test.lum | folder]..."
	filter := ""
	var paths []string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--filter="); ok {
			filter = value
		} else if strings.HasPrefix(arg, "--") {
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n%s\n", arg, usage)
			exit(2)
		} else {
			paths = append(paths, arg)
		}
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}

	files, err := testrun.Find(paths)
	if err != nil {
		fatal(err)
	}
	if len(files) == 0 {
		fmt.Printf("no test files: a test file is called name%s, and its tests fn %sSomething()\n", testrun.Suffix, testrun.Prefix)
		return
	}

	ran, tests, failed, broken := 0, 0, 0, 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			broken++
			continue
		}
		src := string(data)
		found, hasMain := testrun.Tests(src)
		if hasMain {
			fmt.Fprintf(os.Stderr, "%s: a test file has no main, nor statements outside a function: lumen test writes the main that runs its tests\n", file)
			broken++
			continue
		}
		selected := testrun.Matching(found, filter)
		if len(selected) == 0 && !(filter == "" && testrun.Compiles(src) != nil) {
			// Nothing to run here; a file that does not parse is still
			// compiled below, for what is wrong with it to be said
			continue
		}

		count, ok := runTests(file, testrun.Program(src, file, selected), ran > 0)
		if !ok {
			broken++
			continue
		}
		ran++
		tests += len(selected)
		failed += count
	}

	switch {
	case ran == 0 && broken == 0 && filter != "":
		fmt.Printf("no test matches '%s'\n", filter)
	case ran == 0 && broken == 0:
		fmt.Printf("no tests: a test is a function fn %sSomething() of a test file\n", testrun.Prefix)
	case ran+broken > 1:
		summary := fmt.Sprintf("\n%s in %s: %d passed", plural(tests, "test"), plural(ran, "file"), tests-failed)
		if failed > 0 {
			summary += fmt.Sprintf(", %d failed", failed)
		}
		if broken > 0 {
			summary += fmt.Sprintf("; %s did not run", plural(broken, "file"))
		}
		fmt.Println(summary)
	}
	if failed > 0 || broken > 0 {
		exit(1)
	}
}

// plural is a count with its noun: 1 test, 2 tests.
func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// runTests compiles and runs a test file with its main, and returns how
// many of its tests failed. ok is false if it did not get to the end:
// it does not compile, or it stopped outside a test. apart leaves an
// empty line before what the tests print, after those of another file.
func runTests(file, src string, apart bool) (failed int, ok bool) {
	programFile = file
	errs := errors.NewHandler()
	result := compiler.CompileOptions(src, file, projectOptions(file), errs)
	for _, d := range errs.Diagnostics {
		fmt.Fprintln(os.Stderr, d.String())
	}
	if errs.HasErrors() {
		return 0, false
	}
	if apart {
		fmt.Println()
	}

	machine := newVM()
	machine.SetArgs(nil)
	value, err := machine.Exec(result.Chunk)
	if err != nil {
		if vm.IsInterrupted(err) {
			leaveIfAsked(err)
		}
		if status, asked := vm.ExitCode(err); asked {
			// os.exit is not a failure a suite catches: it ends the program
			fmt.Fprintf(os.Stderr, "%s: a test ended the program with os.exit(%d): the tests after it did not run\n", file, status)
			return 0, false
		}
		fmt.Fprint(os.Stderr, strings.TrimRight(err.Error(), "\n")+"\n")
		return 0, false
	}
	return int(value.AsInt()), true
}

// cmdCompile writes the bytecode of a program.
func cmdCompile(inputFile, outputFile string) {
	chunk := compileSource(inputFile).Chunk
	if strip {
		chunk = chunk.Stripped()
	}
	bytecode, err := vm.SerializeChunk(chunk)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(outputFile, bytecode, 0644); err != nil {
		fatal(err)
	}
	fmt.Printf("Compiled %s -> %s\n", inputFile, outputFile)
}

// cmdNative makes an executable of a program: its functions compiled to
// Go, built with the runtime's own source and the go command, which is
// downloaded the first time if the machine has none.
func cmdNative(inputFile, outputFile string) {
	chunk := compileSource(inputFile).Chunk
	if strip {
		chunk = chunk.Stripped()
	}
	if outputFile == "" {
		outputFile = executableName(inputFile)
	}

	const name = "Program"
	generated := native.Generate(native.Program{Name: name, Chunk: chunk})
	main, err := native.Main(name, chunk, runner.Limits{HeapMB: heapMB, MaxDepth: maxDepth})
	if err != nil {
		fatal(err)
	}
	root, err := native.Unpack(lumen.RuntimeSource)
	if err != nil {
		fatal(fmt.Errorf("cannot unpack the runtime: %v", err))
	}
	goCommand, err := native.FindGo(func(what string) { fmt.Println(what) })
	if err != nil {
		fatal(err)
	}
	if err := (native.Source{Root: root, Go: goCommand}).Build(generated.Source, main, outputFile); err != nil {
		fatal(err)
	}
	fmt.Printf("Built %s -> %s (%d of %d functions compiled to Go)\n", inputFile, outputFile, generated.Compiled, generated.Total)
}

// executableName is what lumen native calls the executable of a program
// when not told: the project's name, or the file's, in the current folder.
func executableName(inputFile string) string {
	name := strings.TrimSuffix(filepath.Base(inputFile), ".lum")
	if cfg, err := project.Find(filepath.Dir(inputFile)); err == nil && cfg != nil && cfg.Name != "" {
		name = cfg.Name
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// cmdDump prints what a program is as bytecode, or with --ir the
// intermediate form of its source.
func cmdDump(args []string) {
	usage := "Usage: lumen dump [file.lum | file.lc | project folder] [--ir]"
	ir := false
	var rest []string
	for _, arg := range args {
		if arg == "--ir" {
			ir = true
			continue
		}
		rest = append(rest, arg)
	}
	if len(rest) > 1 {
		fmt.Println(usage)
		exit(1)
	}
	input := sourceFile(firstOf(rest), usage)
	if ir {
		if ext := filepath.Ext(input); ext == ".lc" || ext == ".lasm" {
			fatal(fmt.Errorf("%s: the intermediate form is made from source; dump a .lum file with --ir", input))
		}
		fmt.Print(compileSource(input).IR.Dump())
		return
	}
	fmt.Print(asm.Disassemble(loadProgram(input)))
}

func cmdAssemble(inputFile, outputFile string) {
	// Read input assembly file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		exit(1)
	}

	// Assemble to chunk with debug info
	chunk, err := asm.AssembleFromFile(string(data), inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Assembly error: %v\n", err)
		exit(1)
	}

	// Serialize to bytecode
	bytecode, err := vm.SerializeChunk(chunk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Serialization error: %v\n", err)
		exit(1)
	}

	// Write output file
	if err := os.WriteFile(outputFile, bytecode, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		exit(1)
	}

	fmt.Printf("Assembled %s -> %s (%d bytes)\n", inputFile, outputFile, len(bytecode))
}

func formatValue(val vm.Value) string {
	if val.IsHeapPtr() {
		// Try to dereference and get the actual object
		ptr := val.AsHeapPtr()
		obj := (*vm.ObjHeader)(unsafe.Pointer(ptr))
		if obj != nil {
			switch obj.Type {
			case vm.ObjTypeString:
				strObj := (*vm.StringObj)(unsafe.Pointer(ptr))
				return fmt.Sprintf("String(%q)", strObj.String())
			case vm.ObjTypeArray:
				return "Array(...)"
			case vm.ObjTypeObject:
				return "Object(...)"
			default:
				return fmt.Sprintf("HeapPtr(0x%x)", ptr)
			}
		}
	}
	return val.String()
}

func cmdDebug(inputFile string) {
	chunk := loadProgram(inputFile)

	// Create VM
	v := newVM()

	// Print header
	printDebuggerHeader()

	// Breakpoints for continue mode
	breakpoints := debugger.NewBreakpointManager(chunk)

	// Load breakpoints from source file (marked with ;BREAK comments)
	if len(chunk.Breakpoints) > 0 {
		fmt.Printf("Loaded %d breakpoint(s) from source\n", len(chunk.Breakpoints))
		for _, ip := range chunk.Breakpoints {
			breakpoints.AddBreakpoint(ip)
		}
	}

	continueMode := false
	instrCount := 0

	// Set up debug hook
	v.SetDebugHook(func(snapshot interface{}) bool {
		// Convert snapshot
		s, ok := snapshot.(struct {
			IP            int
			Instr         vm.Instr
			DebugInfo     *vm.DebugInfo
			Registers     []vm.Value
			Constants     []vm.Value
			Code          []vm.Instr
			StackPointer  int
			FramePointer  int
			FrameCount    int
			HeapAllocated uint64
			HeapMax       uint64
			RegisterCount int
			ArgCount      int
		})
		if !ok {
			return false
		}

		instrCount++

		// In continue mode, only stop at breakpoints
		if continueMode {
			if breakpoints.ShouldBreak(s.IP) {
				continueMode = false
				fmt.Printf("\nBreakpoint at IP %d\n", s.IP)
			} else {
				return false // Don't pause
			}
		}

		// Print current state
		printDebuggerState(s.IP, instrCount, s, breakpoints, chunk)

		// Wait for user command
		for {
			fmt.Print("\n> ")
			var input string
			// Read full line including spaces
			fmt.Scanln(&input)

			// If empty, default to step
			if input == "" {
				input = "s"
			}

			handled := handleDebugCommand(input, s.IP, s, breakpoints, chunk, &continueMode)
			if handled {
				return false // Continue execution
			}
		}
	})

	// Execute the chunk with debugging
	result, err := v.Exec(chunk)
	if err != nil {
		leaveIfAsked(err)
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		exit(1)
	}

	fmt.Printf("\nExecution complete: %s (%d instructions)\n", formatValue(result), instrCount)
}

func printDebuggerHeader() {
	fmt.Println("Lumen Debugger")
	fmt.Println("Commands: s=step, c=continue, r=registers, b=breakpoint, i=info, l=labels, h=help, q=quit")
	fmt.Println()
}

func printDebuggerState(ip int, instrCount int, snapshot interface{}, bpm *debugger.BreakpointManager, chunk *vm.Chunk) {
	s := snapshot.(struct {
		IP            int
		Instr         vm.Instr
		DebugInfo     *vm.DebugInfo
		Registers     []vm.Value
		Constants     []vm.Value
		Code          []vm.Instr
		StackPointer  int
		FramePointer  int
		FrameCount    int
		HeapAllocated uint64
		HeapMax       uint64
		RegisterCount int
		ArgCount      int
	})

	fmt.Println()

	// Show basic info
	if bpm.ShouldBreak(ip) {
		fmt.Print("* ")
	}
	fmt.Printf("IP %d  %s", ip, asm.FormatInstruction(s.Instr, chunk))

	// Show function name if available
	if s.DebugInfo != nil && s.DebugInfo.FuncName != "" {
		fmt.Printf("  [%s]", s.DebugInfo.FuncName)
	}
	fmt.Println()

	// Source location (compact)
	if s.DebugInfo != nil && s.DebugInfo.FilePath != "" {
		fmt.Printf("  %s:%d", s.DebugInfo.FilePath, s.DebugInfo.Line)
		if s.DebugInfo.SourceLine != "" {
			fmt.Printf("  %s", strings.TrimSpace(s.DebugInfo.SourceLine))
		}
		fmt.Println()
	}
}

func handleDebugCommand(input string, currentIP int, snapshot interface{}, bpm *debugger.BreakpointManager, chunk *vm.Chunk, continueMode *bool) bool {
	s := snapshot.(struct {
		IP            int
		Instr         vm.Instr
		DebugInfo     *vm.DebugInfo
		Registers     []vm.Value
		Constants     []vm.Value
		Code          []vm.Instr
		StackPointer  int
		FramePointer  int
		FrameCount    int
		HeapAllocated uint64
		HeapMax       uint64
		RegisterCount int
		ArgCount      int
	})

	// Parse command
	parts := splitCommand(input)
	if len(parts) == 0 {
		parts = []string{"s"} // Default to step
	}

	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "s", "step", "":
		return true // Continue execution (step)

	case "c", "continue":
		*continueMode = true
		if bpm.Count() > 0 {
			fmt.Printf("Continuing (%d breakpoint(s) set)\n", bpm.Count())
		}
		return true

	case "b", "break", "breakpoint":
		handleBreakpointCommand(args, currentIP, bpm, chunk)
		return false

	case "r", "regs", "registers":
		handleRegisterCommand(args, s.Registers)
		return false

	case "i", "info":
		printDetailedInfo(s)
		return false

	case "l", "labels":
		handleLabelsCommand(bpm)
		return false

	case "h", "help":
		printDebuggerHeader()
		return false

	case "q", "quit", "exit":
		fmt.Println("Exiting.")
		exit(0)
		return false

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		return false
	}
}

func splitCommand(input string) []string {
	var parts []string
	current := ""

	for _, ch := range input {
		if ch == ' ' || ch == '\t' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

func handleBreakpointCommand(args []string, currentIP int, bpm *debugger.BreakpointManager, chunk *vm.Chunk) {
	if len(args) == 0 {
		fmt.Println("Usage: b <ip> | b list | b clear | b del <ip>")
		return
	}

	subCmd := args[0]

	switch subCmd {
	case "list", "ls", "l":
		bps := bpm.List()
		if len(bps) == 0 {
			fmt.Println("No breakpoints")
			return
		}
		fmt.Printf("Breakpoints:\n")
		for _, ip := range bps {
			debugInfo := chunk.GetDebugInfo(ip)
			location := ""
			if debugInfo != nil {
				if debugInfo.FuncName != "" {
					location = fmt.Sprintf(" [%s]", debugInfo.FuncName)
				}
			}
			fmt.Printf("  IP %d%s\n", ip, location)
		}

	case "clear", "c":
		count := bpm.Count()
		bpm.RemoveAll()
		fmt.Printf("Cleared %d breakpoint(s)\n", count)

	case "del", "delete", "rm", "remove":
		if len(args) < 2 {
			fmt.Println("Usage: b del <ip>")
			return
		}
		var ip int
		_, err := fmt.Sscanf(args[1], "%d", &ip)
		if err != nil {
			fmt.Printf("Invalid IP: %s\n", args[1])
			return
		}
		if !bpm.ShouldBreak(ip) {
			fmt.Printf("No breakpoint at IP %d\n", ip)
			return
		}
		bpm.RemoveBreakpoint(ip)
		fmt.Printf("Removed breakpoint at IP %d\n", ip)

	default:
		// Try to parse as IP
		var ip int
		_, err := fmt.Sscanf(subCmd, "%d", &ip)
		if err != nil {
			fmt.Printf("Invalid IP: %s\n", subCmd)
			return
		}

		if ip < 0 || ip >= len(chunk.Code) {
			fmt.Printf("IP %d out of range (0-%d)\n", ip, len(chunk.Code)-1)
			return
		}

		err = bpm.AddBreakpoint(ip)
		if err != nil {
			fmt.Printf("Failed to set breakpoint: %v\n", err)
			return
		}

		fmt.Printf("Breakpoint set at IP %d\n", ip)
	}
}

func handleRegisterCommand(args []string, registers []vm.Value) {
	if len(args) == 0 {
		// Show all registers
		for i, val := range registers {
			fmt.Printf("r%d = %s\n", i, debugger.InspectValue(val))
		}
		return
	}

	// Show specific register
	var regNum int
	_, err := fmt.Sscanf(args[0], "%d", &regNum)
	if err != nil {
		fmt.Printf("Invalid register: %s\n", args[0])
		return
	}

	if regNum < 0 || regNum >= len(registers) {
		fmt.Printf("Register r%d out of range (0-%d)\n", regNum, len(registers)-1)
		return
	}

	fmt.Printf("r%d = %s\n", regNum, debugger.InspectValue(registers[regNum]))
}

func printDetailedInfo(snapshot interface{}) {
	s := snapshot.(struct {
		IP            int
		Instr         vm.Instr
		DebugInfo     *vm.DebugInfo
		Registers     []vm.Value
		Constants     []vm.Value
		Code          []vm.Instr
		StackPointer  int
		FramePointer  int
		FrameCount    int
		HeapAllocated uint64
		HeapMax       uint64
		RegisterCount int
		ArgCount      int
	})

	fmt.Printf("IP: %d/%d\n", s.IP, len(s.Code))
	fmt.Printf("Opcode: %v  A=%d B=%d C=%d\n", s.Instr.Op, s.Instr.A, s.Instr.B, s.Instr.C)

	if s.DebugInfo != nil {
		if s.DebugInfo.FilePath != "" {
			fmt.Printf("Source: %s:%d\n", s.DebugInfo.FilePath, s.DebugInfo.Line)
		}
		if s.DebugInfo.FuncName != "" {
			fmt.Printf("Function: %s\n", s.DebugInfo.FuncName)
		}
	}

	fmt.Printf("Frames: %d  Heap: %d/%d bytes\n", s.FrameCount, s.HeapAllocated, s.HeapMax)
}

func handleLabelsCommand(bpm *debugger.BreakpointManager) {
	labels := bpm.ListLabels()
	if len(labels) == 0 {
		fmt.Println("No labels")
		return
	}

	for _, label := range labels {
		ip, _ := bpm.GetLabelIP(label)
		fmt.Printf("%-20s  IP %d\n", label, ip)
	}
}
