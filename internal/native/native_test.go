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
