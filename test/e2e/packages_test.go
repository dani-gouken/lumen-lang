package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The package manager, through the commands a user types, against git
// repositories made on the spot: no network is used.

// gitEnv makes git work the same on any machine: no configuration of the
// user, a name for the commits.
func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("LUMEN_CACHE", t.TempDir())
	t.Setenv("LUMEN_PACKAGE_URL", "")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// release commits files to a repository, made if needed, and tags the
// commit; an annotated tag when the tag starts with "a:".
func release(t *testing.T, repo, tag string, files map[string]string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, repo, "init", "--quiet", "--initial-branch=main")
	}
	write(t, repo, files)
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "--quiet", "--allow-empty", "-m", "release "+tag)
	if annotated, ok := strings.CutPrefix(tag, "a:"); ok {
		git(t, repo, "tag", "-a", annotated, "-m", annotated)
	} else if tag != "" {
		git(t, repo, "tag", tag)
	}
}

// lumenOK runs lumen in a folder and fails the test if it fails.
func lumenOK(t *testing.T, dir string, args ...string) result {
	t.Helper()
	res := runLumenIn(t, dir, "", args...)
	if res.exit != 0 {
		t.Fatalf("lumen %v failed: exit %d\nstdout:\n%s\nstderr:\n%s", args, res.exit, res.stdout, res.stderr)
	}
	return res
}

// expectLines checks that each line is in the text, in that order.
func expectLines(t *testing.T, text string, lines ...string) {
	t.Helper()
	rest := text
	for _, line := range lines {
		i := strings.Index(rest, line+"\n")
		if i < 0 {
			t.Errorf("expected the line %q, in order, in\n%s", line, text)
			return
		}
		rest = rest[i+len(line):]
	}
}

type lockFile struct {
	ContentHash string         `json:"content-hash"`
	Packages    []lockedTested `json:"packages"`
	PackagesDev []lockedTested `json:"packages-dev"`
}

type lockedTested struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Import  string `json:"import"`
	Source  struct {
		Type      string `json:"type"`
		URL       string `json:"url"`
		Reference string `json:"reference"`
	} `json:"source"`
	Require map[string]string `json:"require"`
}

func readLock(t *testing.T, dir string) lockFile {
	t.Helper()
	var lock lockFile
	if err := json.Unmarshal([]byte(read(t, dir, "lumen.lock")), &lock); err != nil {
		t.Fatalf("lumen.lock is not JSON: %v", err)
	}
	return lock
}

// versions gives "name version" for each locked package, dev ones marked.
func versions(lock lockFile) string {
	var all []string
	for _, p := range lock.Packages {
		all = append(all, p.Name+" "+p.Version)
	}
	for _, p := range lock.PackagesDev {
		all = append(all, p.Name+" "+p.Version+" (dev)")
	}
	return strings.Join(all, ", ")
}

// The packages of a small ecosystem, as their authors released them.
func publishStrings(t *testing.T, repo string) {
	release(t, repo, "v1.0.0", map[string]string{
		"lumen.json":            `{"name": "acme/strings", "root": "src", "description": "Words made louder"}`,
		"src/strings.lum":       "pub import strings.shout.{loud};\n",
		"src/strings/shout.lum": "pub fn loud(text: String) -> String {\n    return text.upper() + \"!\";\n}\n",
	})
	release(t, repo, "a:v2.0.0", map[string]string{
		"src/strings/shout.lum": "pub fn loud(text: String) -> String {\n    return text.upper() + \"!!\";\n}\n",
	})
	release(t, repo, "v2.1.0", map[string]string{
		"src/strings/shout.lum": "pub fn loud(text: String) -> String {\n    return \"** \" + text.upper() + \" **\";\n}\n",
	})
	release(t, repo, "v3.0.0-beta.1", map[string]string{
		"src/strings/shout.lum": "pub fn loud(text: String) -> String {\n    return \"beta\";\n}\n",
	})
}

func publishGreet(t *testing.T, repo string) {
	// greet imports its own modules through its name, as any project does,
	// and the package it requires through that one's name
	release(t, repo, "1.0.0", map[string]string{
		"lumen.json": `{"name": "acme/greet", "require": {"acme/strings": "^1.0"}}`,
		"greet.lum":  "import strings;\n\npub fn hello(name: String) -> String {\n    return strings.loud(\"hello \" + name);\n}\n",
	})
	release(t, repo, "1.2.0", map[string]string{
		"lumen.json": `{"name": "acme/greet", "description": "Greetings", "require": {"acme/strings": "^2.0"}}`,
	})
}

// A project's life with its packages: required, installed from the lock
// on another machine, kept at its versions until it is updated, a
// requirement that cannot be met refused, a dev package left out, and
// the packages removed.
func TestPackages(t *testing.T) {
	gitEnv(t)
	world := t.TempDir()
	stringsRepo := filepath.Join(world, "repos", "strings")
	greetRepo := filepath.Join(world, "repos", "greet")
	publishStrings(t, stringsRepo)
	publishGreet(t, greetRepo)

	shop := filepath.Join(world, "shop")
	write(t, shop, map[string]string{
		"lumen.json": `{
    "name": "acme/shop",
    "entry": "main.lum",
    "repositories": [
        { "type": "git", "url": "` + filepath.ToSlash(stringsRepo) + `" },
        { "type": "git", "url": "` + filepath.ToSlash(greetRepo) + `" },
        { "type": "path", "url": "../tools" }
    ]
}
`,
		"main.lum": "import greet;\n\nfn main() -> Int {\n    println(greet.hello(\"ada\"));\n    return 0;\n}\n",
	})
	write(t, world, map[string]string{
		"tools/lumen.json": `{"name": "me/tools"}`,
		"tools/tools.lum":  "pub fn twice(n: Int) -> Int {\n    return n * 2;\n}\n",
	})

	// A program that imports a package nobody installed does not compile
	if res := runLumenIn(t, shop, "", "run"); res.exit == 0 || !strings.Contains(res.stderr, "module 'greet' not found") {
		t.Errorf("expected the import to fail before anything is installed: exit %d\n%s", res.exit, res.stderr)
	}

	// 1. lumen require: the newest release, ^ of it in lumen.json, then
	// what it requires
	res := lumenOK(t, shop, "require", "acme/greet")
	expectLines(t, res.stderr,
		"Using version ^1.2 for acme/greet",
		"Updating dependencies",
		"Lock file operations: 2 installs, 0 updates, 0 removals",
		"  - Locking acme/greet (1.2.0)",
		"  - Locking acme/strings (2.1.0)",
		"Writing lock file",
		"Package operations: 2 installs, 0 updates, 0 removals",
		"  - Installing acme/greet (1.2.0)",
		"  - Installing acme/strings (2.1.0)")
	if manifest := read(t, shop, "lumen.json"); !strings.Contains(manifest, "    \"require\": {\n        \"acme/greet\": \"^1.2\"\n    }\n}\n") || !strings.Contains(manifest, `"entry": "main.lum"`) {
		t.Errorf("expected the requirement added and the rest kept:\n%s", manifest)
	}
	lock := readLock(t, shop)
	if versions(lock) != "acme/greet 1.2.0, acme/strings 2.1.0" {
		t.Errorf("unexpected lock: %s", versions(lock))
	}
	// An annotated tag is locked at its commit, not at the tag object
	if got, want := lock.Packages[1].Source.Reference, git(t, stringsRepo, "rev-list", "-n", "1", "v2.1.0"); got != want {
		t.Errorf("expected strings locked at %s, got %s", want, got)
	}
	if lock.Packages[0].Import != "greet" || lock.Packages[0].Require["acme/strings"] != "^2.0" {
		t.Errorf("unexpected locked greet: %+v", lock.Packages[0])
	}

	if run := lumenOK(t, shop, "run"); run.stdout != "** HELLO ADA **\n" {
		t.Errorf("unexpected output: %q", run.stdout)
	}
	// The other commands that compile find the packages too
	lumenOK(t, shop, "check", "main.lum")
	lumenOK(t, shop, "build", "shop.lc")
	if exec := lumenOK(t, shop, "exec", "shop.lc"); !strings.HasPrefix(exec.stdout, "** HELLO ADA **\n") {
		t.Errorf("unexpected output of the bytecode: %q", exec.stdout)
	}

	// 2. New releases do not reach a project until it updates
	release(t, stringsRepo, "v2.2.0", map[string]string{
		"src/strings/shout.lum": "pub fn loud(text: String) -> String {\n    return \">> \" + text.upper();\n}\n",
	})
	release(t, greetRepo, "1.3.0", map[string]string{
		"greet.lum": "import strings;\n\npub fn hello(name: String) -> String {\n    return strings.loud(\"hi \" + name);\n}\n",
	})
	res = lumenOK(t, shop, "install")
	expectLines(t, res.stderr, "Installing dependencies from lock file", "Nothing to install, update or remove")
	if strings.Contains(res.stderr, "Fetching") || strings.Contains(res.stderr, "Cloning") {
		t.Errorf("installing what is installed needs no network:\n%s", res.stderr)
	}

	// 3. Updating one package moves it alone; the other keeps its version
	res = lumenOK(t, shop, "update", "acme/greet")
	expectLines(t, res.stderr,
		"Lock file operations: 0 installs, 1 update, 0 removals",
		"  - Upgrading acme/greet (1.2.0 => 1.3.0)",
		"Package operations: 0 installs, 1 update, 0 removals")
	if run := lumenOK(t, shop, "run"); run.stdout != "** HI ADA **\n" {
		t.Errorf("unexpected output after updating greet: %q", run.stdout)
	}
	res = lumenOK(t, shop, "update")
	expectLines(t, res.stderr, "  - Upgrading acme/strings (2.1.0 => 2.2.0)")
	if strings.Contains(res.stderr, "beta") {
		t.Errorf("a pre-release is not an update:\n%s", res.stderr)
	}
	if run := lumenOK(t, shop, "run"); run.stdout != ">> HI ADA\n" {
		t.Errorf("unexpected output after updating everything: %q", run.stdout)
	}

	// 4. A requirement that cannot be met leaves lumen.json and the lock
	// as they were
	before, lockBefore := read(t, shop, "lumen.json"), read(t, shop, "lumen.lock")
	res = runLumenIn(t, shop, "", "require", "acme/strings:^1.0")
	if res.exit != 1 {
		t.Fatalf("expected the requirement to be refused: exit %d\n%s", res.exit, res.stderr)
	}
	expectLines(t, res.stderr,
		"Installation failed, reverting lumen.json to its original content.",
		"No version of acme/strings meets every requirement on it:",
		"  - lumen.json requires acme/strings ^1.0",
		"  - acme/greet 1.3.0 requires acme/strings ^2.0",
		"  the versions there are: 3.0.0-beta.1, 2.2.0, 2.1.0, 2.0.0, 1.0.0, dev-main")
	if read(t, shop, "lumen.json") != before || read(t, shop, "lumen.lock") != lockBefore {
		t.Error("expected lumen.json and lumen.lock to be left as they were")
	}

	// 5. Asking for an old greet takes the strings it can work with
	lumenOK(t, shop, "require", "acme/greet:~1.0.0")
	if got := versions(readLock(t, shop)); got != "acme/greet 1.0.0, acme/strings 1.0.0" {
		t.Errorf("expected both to go back, got %s", got)
	}
	if run := lumenOK(t, shop, "run"); run.stdout != "HELLO ADA!\n" {
		t.Errorf("unexpected output of the old versions: %q", run.stdout)
	}
	lumenOK(t, shop, "require", "acme/greet:^1.2")

	// 6. A package of a folder, for development only
	res = lumenOK(t, shop, "require", "me/tools", "--dev")
	expectLines(t, res.stderr, "Using version * for me/tools", "  - Locking me/tools (dev-path)")
	write(t, shop, map[string]string{
		"main.lum":      "import greet;\n\nfn main() -> Int {\n    println(greet.hello(\"ada\"));\n    return 0;\n}\n",
		"main_test.lum": "import tools;\nimport std.test;\n\nfn testTwice() {\n    test.equal(tools.twice(21), 42);\n}\n",
	})
	if got := versions(readLock(t, shop)); got != "acme/greet 1.3.0, acme/strings 2.2.0, me/tools dev-path (dev)" {
		t.Errorf("unexpected lock with a dev package: %s", got)
	}
	if res := lumenOK(t, shop, "test"); !strings.Contains(res.stdout+res.stderr, "1 passed") {
		t.Errorf("expected the test to use the dev package:\n%s%s", res.stdout, res.stderr)
	}
	res = lumenOK(t, shop, "show")
	expectLines(t, res.stdout,
		"acme/greet  1.3.0  import greet  Greetings",
		"acme/strings  2.2.0  import strings  Words made louder",
		"me/tools [dev]  dev-path  import tools")

	// Without the dev packages, the program still runs; its test does not
	res = lumenOK(t, shop, "install", "--no-dev")
	expectLines(t, res.stderr, "  - Removing me/tools (dev-path)")
	lumenOK(t, shop, "run")
	if res := runLumenIn(t, shop, "", "test"); res.exit == 0 {
		t.Error("expected the test to fail without the dev package")
	}
	lumenOK(t, shop, "install")

	// 7. lumen.json changed by hand: the lock says so
	manifest := strings.Replace(read(t, shop, "lumen.json"), `"acme/greet": "^1.2"`, `"acme/greet": "^1.3"`, 1)
	write(t, shop, map[string]string{"lumen.json": manifest})
	res = lumenOK(t, shop, "install")
	expectLines(t, res.stderr, "Warning: The lock file is not up to date with the latest changes in lumen.json. You may be getting outdated dependencies. Run lumen update to update them.")
	lumenOK(t, shop, "update")
	if strings.Contains(lumenOK(t, shop, "install").stderr, "Warning") {
		t.Error("expected the warning to go once the lock is updated")
	}

	// 8. Another machine: the project's files, a cache of its own, and
	// the repositories moved on. lumen install gives the locked versions
	release(t, greetRepo, "1.4.0", map[string]string{})
	other := filepath.Join(world, "other", "shop")
	write(t, other, map[string]string{
		"lumen.json": read(t, shop, "lumen.json"),
		"lumen.lock": read(t, shop, "lumen.lock"),
		"main.lum":   read(t, shop, "main.lum"),
	})
	write(t, world, map[string]string{"other/tools/lumen.json": `{"name": "me/tools"}`, "other/tools/tools.lum": ""})
	t.Setenv("LUMEN_CACHE", t.TempDir())
	res = lumenOK(t, other, "install")
	expectLines(t, res.stderr, "  - Installing acme/greet (1.3.0)", "  - Installing acme/strings (2.2.0)")
	if run := lumenOK(t, other, "run"); run.stdout != ">> HI ADA\n" {
		t.Errorf("unexpected output on the other machine: %q", run.stdout)
	}
	if read(t, other, "lumen.lock") != read(t, shop, "lumen.lock") {
		t.Error("installing from the lock changed it")
	}

	// 9. Removing the package removes what only it needed
	res = lumenOK(t, shop, "remove", "acme/greet")
	expectLines(t, res.stderr,
		"Lock file operations: 0 installs, 0 updates, 2 removals",
		"  - Removing acme/greet (1.3.0)",
		"  - Removing acme/strings (2.2.0)")
	if _, err := os.Stat(filepath.Join(shop, "vendor", "acme", "greet")); !os.IsNotExist(err) {
		t.Errorf("expected vendor/acme/greet to be gone, got %v", err)
	}
	if strings.Contains(read(t, shop, "lumen.json"), "acme/greet") {
		t.Error("expected acme/greet out of lumen.json")
	}
}

// A package found at the default place, LUMEN_PACKAGE_URL standing in
// for GitHub, and one required at a branch.
func TestPackageFromTheDefaultPlace(t *testing.T) {
	gitEnv(t)
	world := t.TempDir()
	release(t, filepath.Join(world, "hub", "acme", "dates"), "", map[string]string{
		"lumen.json":   `{"name": "acme/dates", "import": "calendar"}`,
		"calendar.lum": "pub fn weekday() -> String {\n    return \"monday\";\n}\n",
	})
	t.Setenv("LUMEN_PACKAGE_URL", filepath.ToSlash(filepath.Join(world, "hub"))+"/{name}")

	app := filepath.Join(world, "app")
	write(t, app, map[string]string{
		"main.lum": "import calendar;\n\nfn main() -> Int {\n    println(calendar.weekday());\n    return 0;\n}\n",
	})

	// No lumen.json yet: require makes one. A package with no release is
	// required at its branch
	res := lumenOK(t, app, "require", "acme/dates")
	expectLines(t, res.stderr, "Writing a new lumen.json", "Using version dev-main for acme/dates", "  - Locking acme/dates (dev-main)")
	if got := read(t, app, "lumen.json"); got != "{\n    \"require\": {\n        \"acme/dates\": \"dev-main\"\n    }\n}\n" {
		t.Errorf("unexpected lumen.json:\n%s", got)
	}
	if run := lumenOK(t, app, "run", "main.lum"); run.stdout != "monday\n" {
		t.Errorf("unexpected output: %q", run.stdout)
	}

	// A new commit on the branch arrives with an update
	release(t, filepath.Join(world, "hub", "acme", "dates"), "", map[string]string{
		"calendar.lum": "pub fn weekday() -> String {\n    return \"tuesday\";\n}\n",
	})
	res = lumenOK(t, app, "update")
	expectLines(t, res.stderr, "  - Updating acme/dates (dev-main => dev-main)")
	if run := lumenOK(t, app, "run", "main.lum"); run.stdout != "tuesday\n" {
		t.Errorf("unexpected output after the update: %q", run.stdout)
	}
}

func TestPackageErrors(t *testing.T) {
	gitEnv(t)
	world := t.TempDir()
	t.Setenv("LUMEN_PACKAGE_URL", filepath.ToSlash(filepath.Join(world, "nowhere"))+"/{name}")
	write(t, world, map[string]string{
		"app/lumen.json":   `{"entry": "main.lum", "require": {"acme/log": "^1.0"}}`,
		"app/main.lum":     "fn main() -> Int {\n    return 0;\n}\n",
		"empty/readme.txt": "",
		"bad/lumen.json":   `{"require": {"acme/log": "^1.0"}, "repositories": [{"type": "path", "url": "../nothing"}]}`,
	})
	app := filepath.Join(world, "app")

	tests := []struct {
		name string
		dir  string
		args []string
		exit int
		want string
	}{
		{"a project not installed", "app", []string{"run"}, 1,
			"lumen.json requires acme/log, which is not installed: run lumen install"},
		{"no project", "empty", []string{"install"}, 1,
			"there is no lumen.json in this folder or above it: lumen require vendor/name makes one"},
		{"show without a lock", "app", []string{"show"}, 1,
			"there is no lumen.lock: run lumen install"},
		{"a package nowhere", "app", []string{"install"}, 1,
			"acme/log is in none of the repositories of lumen.json, and could not be fetched from " + filepath.ToSlash(filepath.Join(world, "nowhere")) + "/acme/log:"},
		{"a path repository that is not there", "bad", []string{"install"}, 1,
			"the path repository ../nothing has no lumen.json"},
		{"a name that is not a package", "app", []string{"require", "log"}, 1,
			"log is not a package name: write vendor/name, in lower case"},
		{"a constraint that cannot be read", "app", []string{"require", "acme/log:^^1"}, 1,
			`the requirement acme/log "^^1" is not a version constraint`},
		{"removing what is not required", "app", []string{"remove", "acme/json"}, 1,
			"acme/json is not required by lumen.json"},
		{"updating what is not a package of the project", "app", []string{"update", "acme/json"}, 1,
			"acme/json is not a package of this project"},
		{"an unknown option", "app", []string{"install", "--everything"}, 1,
			"unknown option --everything"},
		{"require without a name", "app", []string{"require"}, 1,
			"name the packages to require: lumen require vendor/name[:^1.2]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runLumenIn(t, filepath.Join(world, tt.dir), "", tt.args...)
			if res.exit != tt.exit || !strings.Contains(res.stderr, tt.want) {
				t.Errorf("expected exit %d and %q, got exit %d\nstdout:\n%s\nstderr:\n%s", tt.exit, tt.want, res.exit, res.stdout, res.stderr)
			}
		})
	}

	// A lumen.json that require made for a requirement that fails is not left behind
	write(t, world, map[string]string{"fresh/readme.txt": ""})
	release(t, filepath.Join(world, "nowhere", "acme", "old"), "v1.0.0", map[string]string{"lumen.json": `{"name": "acme/old"}`})
	res := runLumenIn(t, filepath.Join(world, "fresh"), "", "require", "acme/old:^2.0")
	if res.exit != 1 || !strings.Contains(res.stderr, "Installation failed, deleting lumen.json.") {
		t.Errorf("expected the new lumen.json to be deleted: exit %d\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(world, "fresh", "lumen.json")); !os.IsNotExist(err) {
		t.Errorf("expected no lumen.json left, got %v", err)
	}

	// Nothing failed half way: no lock, no vendor
	for _, name := range []string{"lumen.lock", "vendor"} {
		if _, err := os.Stat(filepath.Join(app, name)); !os.IsNotExist(err) {
			t.Errorf("expected no %s after failures, got %v", name, err)
		}
	}
}
