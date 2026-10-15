package native

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GeneratedFile is the file of package vm the functions of a program are
// written to.
const GeneratedFile = "zz_lumen_native.go"

// Source is a tree of Lumen's own source, which a native program is built
// from: its root, where go.mod is.
type Source struct {
	Root string
	Go   string // The go command to build with
}

// Build makes an executable of generated code: the functions of package vm
// and the main package, laid over the source without changing it.
func (s Source) Build(functions, main, output string) error {
	work, err := os.MkdirTemp("", "lumen-native-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	genPath := filepath.Join(work, GeneratedFile)
	mainPath := filepath.Join(work, "main.go")
	if err := os.WriteFile(genPath, []byte(functions), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(mainPath, []byte(main), 0o644); err != nil {
		return err
	}

	// The root as the go command will see it: the real path, not one
	// through a link (/var is /private/var on macOS), or the overlay misses
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	// The program's main is a package of its own in the source tree, and
	// its functions a file of package vm; neither is written there
	pkg := filepath.Join(root, "cmd", "lumen-native-program")
	overlay := map[string]map[string]string{"Replace": {
		filepath.Join(root, "internal", "vm", GeneratedFile): genPath,
		filepath.Join(pkg, "main.go"):                        mainPath,
	}}
	data, err := json.Marshal(overlay)
	if err != nil {
		return err
	}
	overlayPath := filepath.Join(work, "overlay.json")
	if err := os.WriteFile(overlayPath, data, 0o644); err != nil {
		return err
	}

	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	args := []string{"build", "-overlay", overlayPath, "-o", output}
	if _, err := os.Stat(filepath.Join(root, "vendor")); err == nil {
		args = append(args, "-mod=vendor")
	}
	cmd := exec.Command(s.Go, append(args, "./cmd/lumen-native-program")...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
