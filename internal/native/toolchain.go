package native

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CacheDir is where lumen keeps what it downloads and unpacks: LUMEN_CACHE,
// or the cache of the user, as the package manager does.
func CacheDir() string {
	if set := os.Getenv("LUMEN_CACHE"); set != "" {
		return set
	}
	if local := os.Getenv("LOCALAPPDATA"); runtime.GOOS == "windows" && local != "" {
		return filepath.Join(local, "lumen", "cache")
	}
	if home := os.Getenv("XDG_CACHE_HOME"); home != "" {
		return filepath.Join(home, "lumen")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".cache", "lumen")
}

// GoVersion is the Go a native program is built with when the machine has
// none: the one lumen itself was built with.
func GoVersion() string {
	if version := runtime.Version(); strings.HasPrefix(version, "go1.") {
		// A version with a suffix ("go1.26.1 X:...") is the part before it
		return strings.Fields(version)[0]
	}
	return fallbackGo
}

// fallbackGo is the Go downloaded by a lumen built from a development
// version of Go, which go.dev does not have.
const fallbackGo = "go1.26.1"

// DownloadURL is where Go is downloaded from, and its list of files and
// their checksums.
var DownloadURL = "https://go.dev/dl/"

// FindGo returns a go command to build with: the one on the PATH, or the
// one lumen keeps in its cache, downloaded the first time. say tells what
// is being done.
func FindGo(say func(string)) (string, error) {
	if found, err := exec.LookPath("go"); err == nil {
		return found, nil
	}
	version := GoVersion()
	dir := filepath.Join(CacheDir(), "toolchain", version)
	command := filepath.Join(dir, "go", "bin", "go")
	if runtime.GOOS == "windows" {
		command += ".exe"
	}
	if _, err := os.Stat(command); err == nil {
		return command, nil
	}
	say(fmt.Sprintf("Downloading Go %s to build with (once; it is kept in %s)", strings.TrimPrefix(version, "go"), filepath.Dir(dir)))
	if err := download(version, dir); err != nil {
		return "", fmt.Errorf("cannot get Go %s: %v; install Go from https://go.dev/dl and try again", strings.TrimPrefix(version, "go"), err)
	}
	return command, nil
}

// release is what go.dev says of a version: its files.
type release struct {
	Version string `json:"version"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Kind     string `json:"kind"`
		SHA256   string `json:"sha256"`
	} `json:"files"`
}

// download fetches a version of Go for this machine, checks it against the
// checksum go.dev gives, and unpacks it into dir, which it only creates
// once all of it is there.
func download(version, dir string) error {
	listing, err := http.Get(DownloadURL + "?mode=json&include=all")
	if err != nil {
		return err
	}
	defer listing.Body.Close()
	var releases []release
	if err := json.NewDecoder(listing.Body).Decode(&releases); err != nil {
		return fmt.Errorf("the list of Go versions: %v", err)
	}
	filename, sum := "", ""
	for _, r := range releases {
		if r.Version != version {
			continue
		}
		for _, f := range r.Files {
			if f.OS == runtime.GOOS && f.Arch == runtime.GOARCH && f.Kind == "archive" {
				filename, sum = f.Filename, f.SHA256
			}
		}
	}
	if filename == "" {
		return fmt.Errorf("%s has no archive for %s/%s", version, runtime.GOOS, runtime.GOARCH)
	}

	archive, err := os.CreateTemp("", "lumen-go-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	response, err := http.Get(DownloadURL + filename)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", filename, response.Status)
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(archive, hash), response.Body); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != sum {
		return fmt.Errorf("%s is not what go.dev says it is (sha256 %s, expected %s)", filename, got, sum)
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	unpacked, err := os.MkdirTemp(filepath.Dir(dir), "unpacking-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(unpacked)
	if strings.HasSuffix(filename, ".zip") {
		err = unzip(archive.Name(), unpacked)
	} else {
		err = untar(archive.Name(), unpacked)
	}
	if err != nil {
		return err
	}
	return os.Rename(unpacked, dir)
}

// within returns where a file of an archive goes in dir, refusing a name
// that would leave it.
func within(dir, name string) (string, error) {
	path := filepath.Join(dir, filepath.FromSlash(name))
	if !strings.HasPrefix(path, filepath.Clean(dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("the archive names a file outside it: %s", name)
	}
	return path, nil
}

func untar(archive, dir string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	files := tar.NewReader(unzipped)
	for {
		header, err := files.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := within(dir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(path, files, fs.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}
}

func unzip(archive, dir string) error {
	files, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer files.Close()
	for _, f := range files.File {
		path, err := within(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		content, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(path, content, f.Mode())
		content.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, content io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, content); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Unpack writes the source of the runtime into the cache, once for each
// version of it, and returns the root of the tree.
func Unpack(source fs.FS) (string, error) {
	// The tree is named by what is in it: another lumen unpacks its own
	hash := sha256.New()
	err := fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s %d\n", path, len(data))
		hash.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	root := filepath.Join(CacheDir(), "runtime", hex.EncodeToString(hash.Sum(nil))[:16])
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return root, nil
	}

	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", err
	}
	unpacked, err := os.MkdirTemp(filepath.Dir(root), "unpacking-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(unpacked)
	err = fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(unpacked, filepath.FromSlash(path)), strings.NewReader(string(data)), 0o644)
	})
	if err != nil {
		return "", err
	}
	// The folder of the program's main, which the build lays its file
	// into: an overlay adds files, not the folders they are in
	if err := os.MkdirAll(filepath.Join(unpacked, "cmd", "lumen-native-program"), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(unpacked, root); err != nil {
		// Another lumen unpacked the same at the same time
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
			return root, nil
		}
		return "", err
	}
	return root, nil
}
