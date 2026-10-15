package native

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	lumen "github.com/dani-gouken/lumen"
	"github.com/dani-gouken/lumen/internal/vm"
)

// Every package of the module a native program can import, and what they
// import, is in the source lumen carries.
func TestTheRuntimeSourceIsComplete(t *testing.T) {
	list := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}",
		"./internal/runner", "./internal/vm", "./internal/inspect")
	list.Dir = "../.."
	out, err := list.Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	const module = "github.com/dani-gouken/lumen/"
	for _, pkg := range strings.Fields(string(out)) {
		if !strings.HasPrefix(pkg, module) {
			continue
		}
		dir := strings.TrimPrefix(pkg, module)
		if _, err := fs.Stat(lumen.RuntimeSource, dir); err != nil {
			t.Errorf("%s is imported by the runtime and not carried: add it to runtime_source.go", dir)
		}
	}
	for _, file := range []string{"go.mod", "go.sum", "internal/stdlib/lib/strings.lum", "internal/vm/compiled.go"} {
		if _, err := fs.Stat(lumen.RuntimeSource, file); err != nil {
			t.Errorf("%s is not carried", file)
		}
	}
}

func TestUnpackOncePerSource(t *testing.T) {
	t.Setenv("LUMEN_CACHE", t.TempDir())
	source := fstest.MapFS{
		"go.mod":               {Data: []byte("module example\n")},
		"internal/vm/a.go":     {Data: []byte("package vm\n")},
		"internal/stdlib/x.lm": {Data: []byte("text")},
	}
	root, err := Unpack(source)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "internal", "vm", "a.go")); err != nil || string(data) != "package vm\n" {
		t.Errorf("unpacked %q, %v", data, err)
	}

	// The same source is not written again
	marker := filepath.Join(root, "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := Unpack(source)
	if err != nil || again != root {
		t.Fatalf("expected the same tree, got %s, %v", again, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the tree was written again")
	}

	// Other source is a tree of its own
	source["internal/vm/a.go"] = &fstest.MapFile{Data: []byte("package vm // changed\n")}
	other, err := Unpack(source)
	if err != nil || other == root {
		t.Errorf("expected another tree, got %s, %v", other, err)
	}
}

// goServer is go.dev as far as FindGo knows it: the list of versions and
// one archive, whose sum it may misstate.
type goServer struct {
	archive []byte
	sum     string
	served  int
}

func (s *goServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	filename := GoVersion() + "." + runtime.GOOS + "-" + runtime.GOARCH + ext
	if r.URL.Query().Get("mode") == "json" {
		json.NewEncoder(w).Encode([]map[string]any{{
			"version": GoVersion(),
			"files": []map[string]string{
				{"filename": "elsewhere.tar.gz", "os": "plan9", "arch": "386", "kind": "archive", "sha256": "0"},
				{"filename": filename, "os": runtime.GOOS, "arch": runtime.GOARCH, "kind": "archive", "sha256": s.sum},
			},
		}})
		return
	}
	if strings.TrimPrefix(r.URL.Path, "/") != filename {
		http.NotFound(w, r)
		return
	}
	s.served++
	w.Write(s.archive)
}

// goArchive is a .tar.gz of files by name.
func goArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	zipped := gzip.NewWriter(&data)
	archive := tar.NewWriter(zipped)
	for name, content := range files {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		archive.Write([]byte(content))
	}
	archive.Close()
	zipped.Close()
	return data.Bytes()
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestFindGoDownloadsGoOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the archive served is a .tar.gz")
	}
	t.Setenv("PATH", "")
	t.Setenv("LUMEN_CACHE", t.TempDir())
	archive := goArchive(t, map[string]string{"go/bin/go": "#!/bin/sh\necho fake go\n", "go/VERSION": GoVersion()})
	server := &goServer{archive: archive, sum: sumOf(archive)}
	listening := httptest.NewServer(server)
	defer listening.Close()
	defer func(was string) { DownloadURL = was }(DownloadURL)
	DownloadURL = listening.URL + "/"

	var said []string
	command, err := FindGo(func(what string) { said = append(said, what) })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(CacheDir(), "toolchain", GoVersion(), "go", "bin", "go"); command != want {
		t.Errorf("expected %s, got %s", want, command)
	}
	if out, err := exec.Command(command).Output(); err != nil || string(out) != "fake go\n" {
		t.Errorf("the go downloaded runs as %q, %v", out, err)
	}
	if len(said) != 1 || !strings.Contains(said[0], "Downloading Go") {
		t.Errorf("expected to be told of the download, got %q", said)
	}

	// Kept: not downloaded again
	if again, err := FindGo(func(string) { t.Error("downloaded again") }); err != nil || again != command || server.served != 1 {
		t.Errorf("expected the kept go, got %s, %v, %d downloads", again, err, server.served)
	}
}

func TestFindGoRefusesWhatItCannotTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the archive served is a .tar.gz")
	}
	t.Setenv("PATH", "")
	defer func(was string) { DownloadURL = was }(DownloadURL)

	for name, server := range map[string]*goServer{
		"an archive that is not what go.dev says": {archive: goArchive(t, map[string]string{"go/bin/go": "x"}), sum: strings.Repeat("0", 64)},
		"an archive that writes outside its folder": func() *goServer {
			archive := goArchive(t, map[string]string{"../escaped": "x", "go/bin/go": "x"})
			return &goServer{archive: archive, sum: sumOf(archive)}
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			cache := t.TempDir()
			t.Setenv("LUMEN_CACHE", cache)
			listening := httptest.NewServer(server)
			defer listening.Close()
			DownloadURL = listening.URL + "/"

			if _, err := FindGo(func(string) {}); err == nil || !strings.Contains(err.Error(), "install Go from https://go.dev/dl") {
				t.Errorf("expected a refusal that says what to do, got %v", err)
			}
			if _, err := os.Stat(filepath.Join(cache, "toolchain", GoVersion())); err == nil {
				t.Error("something was left where the go would be")
			}
			if _, err := os.Stat(filepath.Join(cache, "toolchain", "escaped")); err == nil {
				t.Error("the archive wrote outside its folder")
			}
		})
	}
}

// A go on the PATH is used as it is.
func TestFindGoUsesTheOneThere(t *testing.T) {
	found, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on the PATH")
	}
	t.Setenv("LUMEN_CACHE", t.TempDir())
	if command, err := FindGo(func(string) { t.Error("downloaded") }); err != nil || command != found {
		t.Errorf("expected %s, got %s, %v", found, command, err)
	}
}

func TestSignatures(t *testing.T) {
	cases := []struct {
		sig     string
		args    int
		params  []kind
		returns kind
	}{
		{"fn(int)->int", 1, []kind{intKind}, intKind},
		{"fn(int,float)->float", 2, []kind{intKind, floatKind}, floatKind},
		{"fn(fn(int,int)->int,[int],float)->string", 3, []kind{other, other, floatKind}, other},
		{"fn({A|B},int?,Map<int,float>)->int?", 3, []kind{other, other, other}, other},
		{"fn(string,...int)->int", 2, []kind{other, other}, intKind},
		{"fn()->fn(int)->int", 0, []kind{}, other},
		{"", 2, []kind{other, other}, other},
		// A signature that does not say as many as the function takes is
		// not believed
		{"fn(int)->int", 2, []kind{other, other}, intKind},
	}
	for _, c := range cases {
		params, returns := signature(c.sig, c.args)
		if len(params) != len(c.params) || returns != c.returns {
			t.Errorf("%q: got %v -> %v", c.sig, params, returns)
			continue
		}
		for i := range params {
			if params[i] != c.params[i] {
				t.Errorf("%q: parameter %d is %v, expected %v", c.sig, i, params[i], c.params[i])
			}
		}
	}
}

func TestWhatRegistersHold(t *testing.T) {
	in := func(op vm.OpCode, a, b, c uint16) vm.Instr { return vm.NewInstr(op, a, b, c) }
	consts := []vm.Value{vm.NewInt(1000003), vm.NewNumber(2.5), vm.NewBool(true)}
	code := []vm.Instr{
		in(vm.OpLOADI, 1, 0, 0),    // 0: r1 = 0, a counter
		in(vm.OpLOADK, 2, 1, 0),    // 1: r2 = 2.5
		in(vm.OpJNLTK, 1, 0, 9),    // 2: while r1 < 1000003
		in(vm.OpMULK, 3, 1, 1),     // 3: r3 = r1 * 2.5, a Float
		in(vm.OpADD, 2, 2, 3),      // 4: r2 += r3
		in(vm.OpADDI, 1, 1, 1),     // 5: r1 += 1
		in(vm.OpMOV, 4, 1, 0),      // 6: r4 = r1, then a Float: either
		in(vm.OpLOADK, 4, 1, 0),    // 7
		in(vm.OpJMP, 2, 0, 0),      // 8
		in(vm.OpCALLK, 5, 0, 1),    // 9: r0 = what function 1 gives
		in(vm.OpMOV, 6, 0, 0),      // 10: r6 = r0
		in(vm.OpLOADK, 7, 2, 0),    // 11: r7 = true
		in(vm.OpLOADI, 8, 3, 0),    // 12: r8 = 3, captured below
		in(vm.OpNEWFUNC, 9, 1, 0),  // 13
		in(vm.OpCAPLOCAL, 9, 0, 8), // 14
		in(vm.OpTONUM, 10, 7, 0),   // 15: a Float, or a failure
		in(vm.OpRET, 2, 0, 0),      // 16
	}
	// A function without arguments: r0 holds what calls give, here Ints
	kinds, local := registerKinds(code, 11, nil, consts, []kind{other, intKind})

	want := map[int]kind{1: intKind, 2: floatKind, 3: floatKind, 4: other, 6: intKind, 7: other, 8: other, 10: floatKind}
	for register, k := range want {
		if kinds[register] != k {
			t.Errorf("r%d holds %v, expected %v", register, kinds[register], k)
		}
	}
	// r0 is never a variable: calls leave results there
	if kinds[0] != intKind || local[0] {
		t.Errorf("r0: %v, a variable: %v", kinds[0], local[0])
	}
	for _, register := range []int{1, 2, 3, 6, 10} {
		if !local[register] {
			t.Errorf("r%d is not a Go variable", register)
		}
	}
	// A register a closure captured is not, whatever was put in it
	if local[8] || local[4] || local[7] {
		t.Errorf("a captured register, or one holding either kind, is a variable: %v", local)
	}
}
