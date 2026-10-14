package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"os/signal"
	"time"

	"github.com/dani-gouken/lumen/internal/asm"
	"github.com/dani-gouken/lumen/internal/compiler"
	"github.com/dani-gouken/lumen/internal/debugger"
	"github.com/dani-gouken/lumen/internal/errors"
	"github.com/dani-gouken/lumen/internal/formatter"
	_ "github.com/dani-gouken/lumen/internal/inspect" // The natives of std.compiler
	"github.com/dani-gouken/lumen/internal/lsp"
	"github.com/dani-gouken/lumen/internal/memguard"
	"github.com/dani-gouken/lumen/internal/project"
	"github.com/dani-gouken/lumen/internal/repl"
	"github.com/dani-gouken/lumen/internal/testrun"
	"github.com/dani-gouken/lumen/internal/vm"
)

// Limits of the virtual machine, settable from the command line and from
// the project file; the command line wins.
var (
	strip    = false // Leave debug information out of what build writes
	heapMB   = 0     // 0: the VM's default
	maxDepth = 0
)

// fatal prints an error and ends the process.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
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
			os.Exit(1)
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
		isOurs := name == "--heap-mb" || name == "--max-depth" || arg == "--strip"
		// fmt and test have options of their own, which they read themselves
		ofTool := len(rest) >= 2 && (rest[1] == "fmt" || rest[1] == "test")
		if !strings.HasPrefix(arg, "--") || ((afterFile || ofTool) && !isOurs) {
			rest = append(rest, arg)
			continue
		}
		if arg == "--strip" {
			strip = true
			continue
		}

		n, err := strconv.Atoi(value)
		if !isOption || err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "Error: option %s needs a positive number, as in %s=64\n", name, name)
			os.Exit(1)
		}
		switch name {
		case "--heap-mb":
			heapMB = n
		case "--max-depth":
			maxDepth = n
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n", name)
			os.Exit(1)
		}
	}
	return rest
}

// newVM creates a virtual machine with the limits from the command line
// and the project file.
func newVM() *vm.VM {
	var machine *vm.VM
	if heapMB > 0 {
		machine = vm.NewVMWithHeapSize(uint64(heapMB) * 1024 * 1024)
	} else {
		machine = vm.NewVM()
	}
	stopOnInterrupt(machine)
	guardMemory(machine)
	if maxDepth > 0 {
		machine.SetMaxCallDepth(maxDepth)
	}
	if len(os.Args) > 3 {
		machine.SetArgs(os.Args[3:])
	}
	machine.SetProgram(programFile)
	return machine
}

// A program runs on the thread lumen started on, and stays there: a
// library that opens a window wants every call about it made from the main
// thread of the process (macOS allows no other), and a function of a C
// library is called from the thread the program is running on.
func init() {
	runtime.LockOSThread()
}

func main() {
	if len(os.Args) > 1 && isProgram(os.Args[1]) {
		// lumen script.lum is lumen run script.lum: what a file that starts
		// with #!/usr/bin/env lumen is run with
		os.Args = append([]string{os.Args[0], "run"}, os.Args[1:]...)
	}
	os.Args = parseOptions(os.Args)
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	command := os.Args[1]

	switch command {
	case "run":
		cmdRun(sourceFile(argument(2), "Usage: lumen run [file.lum | project folder]"))

	case "build":
		// With one argument it is the output, and the input is the project's entry
		usage := "Usage: lumen build [input.lum | project folder] <output.lc|output.lasm>"
		switch len(os.Args) {
		case 3:
			cmdBuild(sourceFile("", usage), os.Args[2])
		case 4:
			cmdBuild(sourceFile(os.Args[2], usage), os.Args[3])
		default:
			fmt.Println(usage)
			os.Exit(1)
		}

	case "ir":
		cmdIR(sourceFile(argument(2), "Usage: lumen ir [file.lum | project folder]"))

	case "repl":
		cmdRepl()

	case "lsp":
		cmdLsp()

	case "fmt":
		cmdFmt()

	case "test":
		cmdTest(os.Args[2:])

	case "asm":
		if len(os.Args) < 4 {
			fmt.Println("Usage: lumen asm <input.lasm> <output.lc>")
			os.Exit(1)
		}
		cmdAssemble(os.Args[2], os.Args[3])

	case "disasm":
		if len(os.Args) < 4 {
			fmt.Println("Usage: lumen disasm <input.lc> <output.lasm>")
			os.Exit(1)
		}
		cmdDisassemble(os.Args[2], os.Args[3])

	case "exec":
		if len(os.Args) < 3 {
			fmt.Println("Usage: lumen exec <file.lc>")
			os.Exit(1)
		}
		cmdExecute(os.Args[2])

	case "runasm":
		if len(os.Args) < 3 {
			fmt.Println("Usage: lumen runasm <file.lasm>")
			os.Exit(1)
		}
		cmdRunAsm(os.Args[2])

	case "lumdbg":
		cmdDebug(sourceFile(argument(2), "Usage: lumen lumdbg [file.lum | file.lasm | file.lc | project folder]"))

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

// commands are the first words lumen understands. A file of that name is
// run with lumen run.
var commands = []string{"run", "build", "ir", "repl", "lsp", "fmt", "test", "asm", "disasm", "exec", "runasm", "lumdbg"}

// isProgram reports whether a first argument names a program to run
// rather than a command: a .lum or .lc file, or any file that is not
// called as a command is (a script without an extension, run through #!).
func isProgram(arg string) bool {
	if strings.HasSuffix(arg, ".lum") || strings.HasSuffix(arg, ".lc") {
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
	fmt.Println("  lumen run <file.lum>       Compile and run a Lumen source file")
	fmt.Println("  lumen run <file.lc>        Run a program built earlier, as its source would run")
	fmt.Println("  lumen <file.lum> [args]    The same as lumen run: what #!/usr/bin/env lumen runs")
	fmt.Println("  lumen build <in> <out>     Compile .lum to .lc bytecode (or .lasm text)")
	fmt.Println("  lumen ir <file.lum>        Print the intermediate representation")
	fmt.Println("  lumen repl                 Type Lumen in a line at a time")
	fmt.Println("  lumen test [files]         Run the tests: the functions test...() of the files *_test.lum")
	fmt.Println("  lumen fmt [files]          Format .lum files where they are; --check only says which would change")
	fmt.Println("  lumen lsp                  Start the language server, for an editor to talk to")
	fmt.Println("  lumen asm <in> <out>       Assemble .lasm to .lc bytecode")
	fmt.Println("  lumen disasm <in> <out>    Disassemble .lc to .lasm text")
	fmt.Println("  lumen exec <file>          Execute .lc bytecode file")
	fmt.Println("  lumen runasm <file>        Assemble and run .lasm file directly")
	fmt.Println("  lumen lumdbg <file>        Debug a .lum, .lasm or .lc file")
	fmt.Println()
	fmt.Println("Options for commands that execute a program:")
	fmt.Println("  --heap-mb=N                Memory the program may use, in MB (default 256)")
	fmt.Println("  --max-depth=N              How deep calls may nest (default 10000)")
	fmt.Println()
	fmt.Println("Option for build:")
	fmt.Println("  --strip                    Leave out what errors use to name the source line:")
	fmt.Println("                             a smaller file, whose errors name the instruction")
	fmt.Println()
	fmt.Println("Projects:")
	fmt.Println("  A lumen.json in the program's folder, or above it, says where the program")
	fmt.Println("  starts (\"entry\"), where module paths start (\"root\"), which packages it")
	fmt.Println("  uses (\"packages\") and its limits (\"heap_mb\", \"max_depth\"). With one,")
	fmt.Println("  run, build, ir and lumdbg take a project folder, or nothing at all:")
	fmt.Println("    lumen run                  Run the project of the current folder")
	fmt.Println("    lumen run path/to/project  Run the project in that folder")
	fmt.Println("    lumen build out.lc         Compile the project of the current folder")
}

// compileSource runs the compilation pipeline on a .lum file. Diagnostics
// are printed to stderr; the process exits if the program does not compile.
func compileSource(inputFile string) *compiler.Result {
	programFile = inputFile
	errs := errors.NewHandler()
	result, err := compiler.CompileFileOptions(inputFile, projectOptions(inputFile), errs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, d := range errs.Diagnostics {
		fmt.Fprintln(os.Stderr, d.String())
	}
	if errs.HasErrors() {
		os.Exit(1)
	}
	return result
}

// leaveIfAsked ends the process with the status the program asked for, if
// the run ended by a call of std.os.exit and not by a failure. A program
// stopped from the keyboard ends as the shell expects, having said where
// it was.
func leaveIfAsked(err error) {
	if status, asked := vm.ExitCode(err); asked {
		os.Exit(status & 0xFF)
	}
	if vm.IsInterrupted(err) {
		fmt.Fprint(os.Stderr, strings.TrimRight(err.Error(), "\n")+"\n")
		os.Exit(interruptedStatus)
	}
}

// guardMemory ends the process if it comes to hold far more than the
// program may. The VM refuses a program more values than its limit; this
// is for what that count does not cover, and would otherwise only stop
// when the machine has no memory left: several times the limit, and room
// for lumen itself.
func guardMemory(machine *vm.VM) {
	const own = 1 << 30
	memguard.Enforce(4*machine.GetHeap().MaxBytes()+own, "the program")
}

// interruptedStatus is what a process stopped by Ctrl-C ends with: 128
// plus the number of the signal.
const interruptedStatus = 130

// stopOnInterrupt makes Ctrl-C stop the program a VM runs rather than the
// process: the program ends with an error that says where it was, which
// is what one wants to know of a program that would not end. A program
// that does not get to it, because it waits for something that does not
// come, is ended the plain way a moment later; so is one interrupted twice.
func stopOnInterrupt(machine *vm.VM) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)
	go func() {
		<-signals
		machine.Interrupt()
		select {
		case <-signals:
		case <-time.After(2 * time.Second):
		}
		os.Exit(interruptedStatus)
	}()
}

// programFile is the file being run, for the program to find what comes
// with it (std.os.program).
var programFile string

// cmdRun compiles and executes a .lum file. An Int returned by main becomes
// the process exit code.
//
// A .lc file built earlier is run the same way: what `lumen build` made of
// a program does what the program does, ends with the status it ends with
// and writes nothing of its own. (`lumen exec` is the tool for looking at
// a bytecode file: it says what main returned.)
func cmdRun(inputFile string) {
	var chunk *vm.Chunk
	if strings.HasSuffix(inputFile, ".lc") {
		programFile = inputFile
		data, err := os.ReadFile(inputFile)
		if err != nil {
			fatal(err)
		}
		if chunk, err = vm.DeserializeChunk(data); err != nil {
			fatal(fmt.Errorf("%s: %v", inputFile, err))
		}
	} else {
		chunk = compileSource(inputFile).Chunk
	}

	result, err := newVM().Exec(chunk)
	if err != nil {
		leaveIfAsked(err)
		fmt.Fprint(os.Stderr, strings.TrimRight(err.Error(), "\n")+"\n")
		os.Exit(1)
	}
	if result.IsInt() {
		os.Exit(int(result.AsInt() & 0xFF))
	}
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
		guardMemory(m)
		return m
	}
	os.Exit(repl.Run(opts))
}

// runTool runs a Lumen program kept in the binary, with what follows the
// command as its arguments, and returns the status it ended with: what
// its main returned, or 1 if it failed.
func runTool(source, file string) int {
	errs := errors.NewHandler()
	result := compiler.Compile(source, file, errs)
	if errs.HasErrors() {
		for _, d := range errs.Diagnostics {
			fmt.Fprintln(os.Stderr, d.String())
		}
		return 1
	}
	programFile = file
	machine := newVM()
	machine.SetArgs(os.Args[2:])
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
	os.Exit(1)
}

// cmdFmt runs the formatter: a Lumen program too.
func cmdFmt() {
	os.Exit(runTool(formatter.Source, formatter.File))
}

// cmdTest runs the tests of the files and folders named, or of the
// current folder: every function test...() of every file *_test.lum.
// Each file is a program of its own, compiled with a main that runs its
// tests (see internal/testrun). The process ends with 1 if a test failed
// or a file could not be run.
func cmdTest(args []string) {
	const usage = "Usage: lumen test [--filter=text] [file_test.lum | folder]..."
	filter := ""
	var paths []string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--filter="); ok {
			filter = value
		} else if strings.HasPrefix(arg, "--") {
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n%s\n", arg, usage)
			os.Exit(2)
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
			fmt.Fprintf(os.Stderr, "%s: a test file has no main: lumen test writes the one that runs its tests\n", file)
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
		os.Exit(1)
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

// cmdBuild compiles a .lum file to bytecode, or to assembly text when the
// output name ends in .lasm.
func cmdBuild(inputFile, outputFile string) {
	chunk := compileSource(inputFile).Chunk

	var output []byte
	if strings.HasSuffix(outputFile, ".lasm") {
		output = []byte(asm.Disassemble(chunk))
	} else {
		if strip {
			chunk = chunk.Stripped()
		}
		bytecode, err := vm.SerializeChunk(chunk)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Serialization error: %v\n", err)
			os.Exit(1)
		}
		output = bytecode
	}

	if err := os.WriteFile(outputFile, output, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Compiled %s -> %s\n", inputFile, outputFile)
}

// cmdIR prints the intermediate representation of a .lum file.
func cmdIR(inputFile string) {
	fmt.Print(compileSource(inputFile).IR.Dump())
}

func cmdAssemble(inputFile, outputFile string) {
	// Read input assembly file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Assemble to chunk with debug info
	chunk, err := asm.AssembleFromFile(string(data), inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Assembly error: %v\n", err)
		os.Exit(1)
	}

	// Serialize to bytecode
	bytecode, err := vm.SerializeChunk(chunk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Serialization error: %v\n", err)
		os.Exit(1)
	}

	// Write output file
	if err := os.WriteFile(outputFile, bytecode, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Assembled %s -> %s (%d bytes)\n", inputFile, outputFile, len(bytecode))
}

func cmdDisassemble(inputFile, outputFile string) {
	// Read input bytecode file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Deserialize to chunk
	chunk, err := vm.DeserializeChunk(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Deserialization error: %v\n", err)
		os.Exit(1)
	}

	// Disassemble to text
	asmText := asm.Disassemble(chunk)

	// Write output file
	if err := os.WriteFile(outputFile, []byte(asmText), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Disassembled %s -> %s\n", inputFile, outputFile)
}

func cmdExecute(inputFile string) {
	programFile = inputFile
	// Read bytecode file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Deserialize to chunk
	chunk, err := vm.DeserializeChunk(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Deserialization error: %v\n", err)
		os.Exit(1)
	}

	// Create VM and execute
	v := newVM()
	result, err := v.Exec(chunk)
	if err != nil {
		leaveIfAsked(err)
		fmt.Fprintf(os.Stderr, "Execution error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Result: %s\n", formatValue(result))
}

func cmdRunAsm(inputFile string) {
	programFile = inputFile
	// Read assembly file
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Assemble to chunk
	chunk, err := asm.AssembleFromFile(string(data), inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Assembly error: %v\n", err)
		os.Exit(1)
	}

	// Create VM and execute directly
	v := newVM()
	result, err := v.Exec(chunk)
	if err != nil {
		leaveIfAsked(err)
		fmt.Fprintf(os.Stderr, "Execution error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Result: %s\n", formatValue(result))
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
	programFile = inputFile
	var chunk *vm.Chunk

	// Determine if file is .lum, .lc or .lasm
	if strings.HasSuffix(inputFile, ".lum") {
		chunk = compileSource(inputFile).Chunk
	} else if len(inputFile) > 3 && inputFile[len(inputFile)-3:] == ".lc" {
		// Load bytecode file
		data, err := os.ReadFile(inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}

		chunk, err = vm.DeserializeChunk(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Deserialization error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Assume .lasm file
		data, err := os.ReadFile(inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}

		chunk, err = asm.AssembleFromFile(string(data), inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Assembly error: %v\n", err)
			os.Exit(1)
		}
	}

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
		os.Exit(1)
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
		os.Exit(0)
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
