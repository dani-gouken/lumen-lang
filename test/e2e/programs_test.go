package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProgramArguments checks what a program is started with: what
// follows the file is its own, except the options of lumen itself.
func TestProgramArguments(t *testing.T) {
	dir := project(t, map[string]string{
		"args.lum": `
			import std.os;
			import std.path;
			import std.arrays;
			fn main() -> Int {
				println(os.args(), path.base(os.program()), path.isAbsolute(os.program()) || path.parts(os.program()).len() > 0);
				return os.args().len();
			}`,
		"lumen.json": `{"name": "args", "entry": "args.lum"}`,
	})
	file := filepath.Join(dir, "args.lum")

	cases := []struct {
		args []string
		want string
		exit int
	}{
		{[]string{"run", file}, `[] args.lum true`, 0},
		{[]string{"run", file, "add", "two words", ""}, `["add", "two words", ""] args.lum true`, 3},
		// Options of the program pass through, with or without a value
		{[]string{"run", file, "--all", "--due=2026-10-20", "-x"}, `["--all", "--due=2026-10-20", "-x"] args.lum true`, 3},
		// The options of lumen are taken out wherever they are
		{[]string{"run", "--max-depth=500", file, "a", "--heap-mb=64", "b"}, `["a", "b"] args.lum true`, 2},
		// ...unless they come after --, which is for the program too
		{[]string{"run", file, "a", "--", "--heap-mb=1", "--", "b"}, `["a", "--heap-mb=1", "--", "b"] args.lum true`, 4},
		// A project is run by its folder
		{[]string{"run", dir, "x"}, `["x"] args.lum true`, 1},
	}
	for _, c := range cases {
		res := runLumen(t, c.args...)
		if strings.TrimSpace(res.stdout) != c.want || res.exit != c.exit || res.stderr != "" {
			t.Errorf("lumen %v:\n  want %s, exit %d\n   got %s, exit %d\n%s", c.args, c.want, c.exit, strings.TrimSpace(res.stdout), res.exit, res.stderr)
		}
	}

	// Compiled bytecode is given arguments the same way
	bytecode := filepath.Join(t.TempDir(), "args.lc")
	if res := runLumen(t, "compile", file, "-o", bytecode); res.exit != 0 {
		t.Fatalf("compile failed:\n%s", res.stderr)
	}
	if res := runLumen(t, "run", bytecode, "one", "--two"); !strings.Contains(res.stdout, `["one", "--two"] args.lc true`) {
		t.Errorf("expected the file to pass the arguments on, got %q\n%s", res.stdout, res.stderr)
	}
}

// TestExitStatus checks that os.exit ends the process with its status,
// from wherever it is called, without an error message.
func TestExitStatus(t *testing.T) {
	dir := project(t, map[string]string{
		"exit.lum": `
			import std.os;
			import std.strings;
			import std.result.{attempt};
			fn leave(status: Int) -> Int {
				os.exit(status);
				return 99;
			}
			fn main() -> Int {
				let status = os.args()[0].parseInt() ?? 0;
				println("before");
				if os.args()[1] == "caught" {
					let outcome = attempt(fn() -> Int { return leave(status); });
					println("after", outcome.isOk());
				} else {
					leave(status);
				}
				println("not reached");
				return 50;
			}`,
	})
	file := filepath.Join(dir, "exit.lum")
	for _, c := range []struct {
		status, how string
		exit        int
	}{{"0", "plain", 0}, {"3", "plain", 3}, {"7", "caught", 7}, {"255", "plain", 255}, {"256", "plain", 0}} {
		res := runLumen(t, "run", file, c.status, c.how)
		if res.exit != c.exit || res.stdout != "before\n" || res.stderr != "" {
			t.Errorf("exit(%s), %s: expected status %d after 'before', got %d, %q\n%s", c.status, c.how, c.exit, res.exit, res.stdout, res.stderr)
		}
	}
}

// TestTodoExample drives the to-do list as a person would, one command
// per process, with the list kept in a file between them.
func TestTodoExample(t *testing.T) {
	list := filepath.Join(t.TempDir(), "list.json")
	t.Setenv("TODO_FILE", list)
	todo := func(args ...string) result {
		t.Helper()
		return runLumen(t, append([]string{"run", "examples/todo"}, args...)...)
	}

	if res := todo("list"); res.exit != 0 || res.stdout != "nothing to do\n" {
		t.Errorf("expected an empty list at first, got %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	if res := todo("add", "Buy milk", "--due=2020-01-01", "--priority=high"); res.exit != 0 || res.stdout != "added 1: Buy milk\n" {
		t.Errorf("unexpected answer to add: %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	if res := todo("add", "Call", "the", "bank"); res.stdout != "added 2: Call the bank\n" {
		t.Errorf("expected the words of a title to be joined, got %q", res.stdout)
	}

	// The file is JSON anyone can read
	var saved []map[string]interface{}
	data, err := os.ReadFile(list)
	if err != nil || json.Unmarshal(data, &saved) != nil || len(saved) != 2 {
		t.Fatalf("expected two tasks in %s, got %s (%v)", list, data, err)
	}
	if saved[0]["title"] != "Buy milk" || saved[0]["priority"] != "high" || saved[0]["due"] != "2020-01-01" || saved[1]["id"] != float64(2) {
		t.Errorf("unexpected content: %s", data)
	}

	res := todo("list")
	if !strings.Contains(res.stdout, "1  !  Buy milk") || !strings.Contains(res.stdout, "days late") || !strings.Contains(res.stdout, "2 tasks to do, 1 late") {
		t.Errorf("expected the late task first, got:\n%s", res.stdout)
	}
	if res := todo("done", "1"); res.exit != 0 || res.stdout != "done: Buy milk\n" {
		t.Errorf("unexpected answer to done: %d, %q", res.exit, res.stdout)
	}
	if res := todo("list"); strings.Contains(res.stdout, "Buy milk") || !strings.Contains(res.stdout, "1 task to do, 0 late") {
		t.Errorf("expected the finished task to be hidden, got:\n%s", res.stdout)
	}
	if res := todo("list", "--all"); !strings.Contains(res.stdout, "1  x  Buy milk") {
		t.Errorf("expected --all to show it, got:\n%s", res.stdout)
	}

	// Mistakes are answered on the terminal with status 1, and change nothing
	before, _ := os.ReadFile(list)
	for args, want := range map[string]string{
		"done 9":            "todo: there is no task 9",
		"done x":            "todo: 'x' is not the number of a task",
		"add":               "todo: add what? Give the task a title",
		"add Fly --due=x":   "todo: 'x' is not a date: write it as 2026-10-20",
		"add Fly --when=x":  "todo: add does not know the option --when",
		"launch the rocket": "todo: there is no command 'launch'; try help",
	} {
		if res := todo(strings.Fields(args)...); res.exit != 1 || strings.TrimSpace(res.stdout) != want {
			t.Errorf("todo %s: expected %q and status 1, got %q and %d", args, want, res.stdout, res.exit)
		}
	}
	if after, _ := os.ReadFile(list); string(after) != string(before) {
		t.Errorf("expected refused commands to leave the list alone, got:\n%s", after)
	}

	// A list that cannot be read is not replaced by an empty one
	if err := os.WriteFile(list, []byte(`[{"id": 1, "title": `), 0644); err != nil {
		t.Fatal(err)
	}
	if res := todo("add", "x"); res.exit != 1 || !strings.Contains(res.stdout, "the list is damaged at line 1, column 21") {
		t.Errorf("expected a damaged list to be reported, got %d, %q", res.exit, res.stdout)
	}
	if after, _ := os.ReadFile(list); string(after) != `[{"id": 1, "title": ` {
		t.Errorf("expected the damaged list to be left as it was, got %q", after)
	}
}

// TestSalesAndLogsExamples runs the two reports on files of the test's own.
func TestSalesAndLogsExamples(t *testing.T) {
	dir := t.TempDir()
	orders := filepath.Join(dir, "orders.csv")
	summary := filepath.Join(dir, "out", "summary.json")
	content := "order,date,customer,product,category,quantity,unit_price\n" +
		"1,2026-10-05,\"Doe, Jane\",Pen,stationery,10,1.50\n" +
		"2,2026-10-06,John,Desk,furniture,1,200\n" +
		"3,soon,John,Desk,furniture,1,200\n"
	if err := os.WriteFile(orders, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// The folder of the summary does not exist: said, with status 1
	res := runLumen(t, "run", "examples/sales", orders, summary)
	if res.exit != 1 || !strings.Contains(res.stdout, "cannot write "+summary+": no such file or directory") {
		t.Errorf("expected the summary to be refused, got %d:\n%s%s", res.exit, res.stdout, res.stderr)
	}
	if err := os.Mkdir(filepath.Dir(summary), 0755); err != nil {
		t.Fatal(err)
	}
	res = runLumen(t, "run", "examples/sales", orders, summary)
	for _, want := range []string{
		"2 orders over 2 days, 215.00 in all, 107.50 an order",
		"furniture        1      1   200.00  93.0%",
		"Doe, Jane       1     10    15.00   7.0%",
		"1 row left out of 3",
		"row 4 (order 3): 'soon' is not a date",
		"summary written to summary.json, sha256 ",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("expected %q in the report, got:\n%s%s", want, res.stdout, res.stderr)
		}
	}
	var written struct {
		From, To  string
		Orders    int
		LeftOut   int `json:"left_out"`
		Customers []struct {
			Name    string
			Revenue float64
		}
	}
	data, err := os.ReadFile(summary)
	if err != nil || json.Unmarshal(data, &written) != nil {
		t.Fatalf("expected a summary in JSON, got %s (%v)", data, err)
	}
	if written.From != "2026-10-05" || written.Orders != 2 || written.LeftOut != 1 || len(written.Customers) != 2 || written.Customers[0].Name != "John" || written.Customers[1].Revenue != 15 {
		t.Errorf("unexpected summary: %+v", written)
	}

	for _, c := range [][2]string{
		{"order,date\n1\n", "orders.csv, line 2, column 1: expected 2 values, as in the header, found 1"},
		{"a,\"b\n", "orders.csv, line"},
		{"order,date,customer,product,category,quantity,unit_price\n", "no order could be read from"},
	} {
		os.WriteFile(orders, []byte(c[0]), 0644)
		if res := runLumen(t, "run", "examples/sales", orders); res.exit != 1 || !strings.Contains(res.stdout, c[1]) {
			t.Errorf("%q: expected %q and status 1, got %d:\n%s", c[0], c[1], res.exit, res.stdout)
		}
	}

	// The log report, on a log of two lines, one of them broken
	log := filepath.Join(dir, "access.log")
	os.WriteFile(log, []byte("10.0.0.1 - bob [01/Jan/2026:23:59:59 +0100] \"GET /a/7?x=1 HTTP/1.1\" 200 2048 1.5\n\ngarbage\n"), 0644)
	res = runLumen(t, "run", "examples/logs", log)
	for _, want := range []string{
		"Access log, Thursday 1 January 2026",
		"1 request from 1 client, 22:59 to 22:59", // told in UTC
		"2.0 KB sent",
		"/a/:id     1  1.5s          0",
		"bob: 1 request, 0 of them changing something",
		"GET /a/7 took 1.5s at 22:59:59 (status 200)",
		"line 3: not a log line: garbage...",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("expected %q in the report, got:\n%s%s", want, res.stdout, res.stderr)
		}
	}
	if res := runLumen(t, "run", "examples/logs", filepath.Join(dir, "none.log")); res.exit != 1 || !strings.Contains(res.stderr, "none.log: no such file or directory") {
		t.Errorf("expected a missing log to be reported, got %d: %s", res.exit, res.stderr)
	}
}

// lockedText collects what a process prints while the test reads it.
type lockedText struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *lockedText) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.Write(p)
}

func (l *lockedText) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// TestNotesService starts the notes example as a server and talks to it
// over HTTP, as the comment at its top tells a person to.
func TestNotesService(t *testing.T) {
	// A port that is free right now
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	server := exec.Command(lumen, "run", "examples/notes", "serve", fmt.Sprint(port))
	server.Dir = repoRoot
	output, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		server.Process.Kill()
		server.Wait()
	}()
	printed := &lockedText{}
	go io.Copy(printed, output)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	ask := func(method, path, body string) (int, string, http.Header) {
		t.Helper()
		request, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		var response *http.Response
		// The first request may come before the server listens
		for attempt := 0; attempt < 100; attempt++ {
			if response, err = http.DefaultClient.Do(request); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
			request, _ = http.NewRequest(method, base+path, strings.NewReader(body))
		}
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer response.Body.Close()
		content, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(content), response.Header
	}
	expect := func(method, path, body string, status int, want string) {
		t.Helper()
		if got, content, _ := ask(method, path, body); got != status || content != want {
			t.Errorf("%s %s: expected %d %s, got %d %s", method, path, status, want, got, content)
		}
	}

	expect("GET", "/notes", "", 200, `[]`)
	status, content, header := ask("POST", "/notes", `{"title": "Groceries", "body": "milk", "tags": ["Home", "home"]}`)
	if status != 201 || content != `{"id":1,"title":"Groceries","body":"milk","tags":["home"]}` || header.Get("Location") != "/notes/1" || header.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected answer to the first note: %d %s %v", status, content, header)
	}
	expect("POST", "/notes", `{"title": "Standup", "tags": ["work"]}`, 201, `{"id":2,"title":"Standup","body":"","tags":["work"]}`)
	expect("POST", "/notes", `{"title": "GROCERIES"}`, 409, `{"error":"there is already a note called 'Groceries'"}`)
	expect("POST", "/notes", `{"title": `, 400, `{"error":"the body is not JSON: line 1, column 11: the text ends before the value does"}`)
	expect("GET", "/notes?tag=work", "", 200, `[{"id":2,"title":"Standup","body":"","tags":["work"]}]`)
	expect("GET", "/notes?q=MILK", "", 200, `[{"id":1,"title":"Groceries","body":"milk","tags":["home"]}]`)
	expect("GET", "/tags", "", 200, `["home","work"]`)
	expect("PUT", "/notes/2", `{"title": "Standup", "body": "at ten"}`, 200, `{"id":2,"title":"Standup","body":"at ten","tags":[]}`)
	expect("DELETE", "/notes/1", "", 204, "")
	expect("GET", "/notes/1", "", 404, `{"error":"there is no note 1"}`)
	expect("PATCH", "/notes/2", "", 405, "method not allowed")
	expect("GET", "/elsewhere", "", 404, "not found")
	// The state is kept between requests: one program, one book
	expect("GET", "/notes", "", 200, `[{"id":2,"title":"Standup","body":"at ten","tags":[]}]`)

	// Every request was noted as it was answered
	time.Sleep(100 * time.Millisecond)
	log := printed.String()
	for _, want := range []string{"notes are served on http://localhost:", " POST /notes -> 201 in ", " GET /notes/1 -> 404 in ", " PATCH /notes/2 -> 405 in "} {
		if !strings.Contains(log, want) {
			t.Errorf("expected %q in what the server printed, got:\n%s", want, log)
		}
	}

	// A second server on the same port says why it cannot start
	res := runLumen(t, "run", "examples/notes", "serve", fmt.Sprint(port))
	if res.exit != 1 || !strings.Contains(res.stdout, "the port is taken") {
		t.Errorf("expected the port to be reported as taken, got %d: %s%s", res.exit, res.stdout, res.stderr)
	}
}
