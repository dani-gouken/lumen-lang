package e2e

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestDamagedBytecodeIsRefused runs compiled programs that were damaged on
// the way: lumen says what is wrong with the file, and never fails itself.
func TestDamagedBytecodeIsRefused(t *testing.T) {
	dir := project(t, map[string]string{
		"sum.lum": `
			struct Pair { left: Int; right: Int; }
			fn add(pair: Pair) -> Int { return pair.left + pair.right; }
			fn main() -> Int {
				let total = 0;
				for i in 0..10 { total += add(Pair { left: i, right: 1 }); }
				println(total);
				return 0;
			}`,
	})
	bytecode := filepath.Join(dir, "sum.lc")
	if res := runLumen(t, "build", filepath.Join(dir, "sum.lum"), bytecode); res.exit != 0 {
		t.Fatalf("build failed:\n%s", res.stderr)
	}
	if res := runLumen(t, "exec", bytecode); res.exit != 0 || !strings.HasPrefix(res.stdout, "55\n") {
		t.Fatalf("the file as built: exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	whole, err := os.ReadFile(bytecode)
	if err != nil {
		t.Fatal(err)
	}
	constants := int(binary.LittleEndian.Uint32(whole[8:]))
	code := 8 + 4 + constants*8 + 4 // Where the instructions start

	damage := map[string]func([]byte) []byte{
		"cut in the middle":       func(data []byte) []byte { return data[:len(data)/2] },
		"cut after the header":    func(data []byte) []byte { return data[:8] },
		"a register out of reach": func(data []byte) []byte { binary.LittleEndian.PutUint16(data[code+1:], 60000); return data },
		"an instruction unknown":  func(data []byte) []byte { data[code] = 250; return data },
		"more constants than bytes": func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[8:], 0xFFFFFFFF)
			return data
		},
		"another version": func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4:], 999); return data },
		"not bytecode":    func(data []byte) []byte { return []byte("fn main() -> Int { return 0; }") },
	}
	for name, change := range damage {
		t.Run(name, func(t *testing.T) {
			damaged := filepath.Join(t.TempDir(), "damaged.lc")
			if err := os.WriteFile(damaged, change(append([]byte(nil), whole...)), 0644); err != nil {
				t.Fatal(err)
			}
			res := runLumen(t, "exec", damaged)
			if res.exit != 1 || res.stdout != "" {
				t.Errorf("expected a refusal: exit %d, wrote %q", res.exit, res.stdout)
			}
			if strings.Contains(res.stderr, "goroutine") || strings.Contains(res.stderr, "panic") {
				t.Errorf("lumen itself failed:\n%s", res.stderr)
			}
			if !strings.Contains(res.stderr, "bytecode") && !strings.Contains(res.stderr, "magic") {
				t.Errorf("the message does not say the file is at fault:\n%s", res.stderr)
			}
		})
	}
}

// watched is what a program writes, readable while it is still running.
type watched struct {
	mu   sync.Mutex
	text bytes.Buffer
}

func (w *watched) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.text.Write(p)
}

func (w *watched) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.text.String()
}

// TestInterruptedFromTheKeyboard stops programs that would not end, the way
// Ctrl-C does: lumen says where the program was and ends with the status a
// shell expects of an interrupted command.
func TestInterruptedFromTheKeyboard(t *testing.T) {
	dir := project(t, map[string]string{
		"spin.lum": `
			fn collatz(start: Int) -> Int {
				let n = start;
				let steps = 0;
				while n != 1 || steps >= 0 {
					if n % 2 == 0 { n = n / 2; } else { n = 3 * n + 1; }
					steps += 1;
				}
				return steps;
			}
			fn main() -> Int {
				println("started");
				return collatz(27);
			}`,
		"sleep.lum": `
			import std.time;
			fn main() -> Int {
				println("started");
				time.sleep(600000);
				println("woke");
				return 0;
			}`,
	})

	for _, c := range []struct{ file, where string }{
		{"spin.lum", "in function 'collatz'"},
		{"sleep.lum", "in function 'main'"},
	} {
		t.Run(c.file, func(t *testing.T) {
			var stdout, stderr watched
			cmd := exec.Command(lumen, "run", filepath.Join(dir, c.file))
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()

			// Once it has started running, interrupt it
			deadline := time.Now().Add(runTimeout)
			for !strings.Contains(stdout.String(), "started") && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			cmd.Process.Signal(syscall.SIGINT)

			select {
			case <-done:
			case <-time.After(runTimeout):
				cmd.Process.Kill()
				t.Fatal("the program went on after the interruption")
			}
			if status := cmd.ProcessState.ExitCode(); status != 130 {
				t.Errorf("expected exit 130, got %d", status)
			}
			if strings.Contains(stdout.String(), "woke") {
				t.Error("the program went on to its next line")
			}
			for _, want := range []string{"R0011", "interrupted", c.where, c.file} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("expected %q in:\n%s", want, stderr.String())
				}
			}
		})
	}
}

// TestALimitedHeapIsEnough runs a program that makes far more than its
// limit over time, and keeps little: memory that is given back is counted
// as given back, so the limit is only about what is alive at once.
func TestALimitedHeapIsEnough(t *testing.T) {
	dir := project(t, map[string]string{
		"churn.lum": `
			import std.arrays;
			import std.strings;

			struct Reading { sensor: String; value: Int; }

			fn average(readings: [Reading]) -> Int {
				let sum = 0;
				for reading in readings { sum += reading.value; }
				return sum / readings.len();
			}

			fn main() -> Int {
				let best = 0;
				for batch in 0..3000 {
					let readings: [Reading] = [];
					for i in 0..100 {
						readings.push(Reading { sensor: "sensor-" + toString(i), value: (batch * 31 + i * 17) % 1000 });
					}
					let scale = batch % 3 + 1;
					let scaled = readings.map(r -> Reading { sensor: r.sensor.upper(), value: r.value * scale });
					let mean = average(scaled);
					if mean > best { best = mean; }
				}
				println(best);
				return 0;
			}`,
	})
	file := filepath.Join(dir, "churn.lum")

	res := runLumen(t, "run", "--heap-mb=2", file)
	if res.exit != 0 || strings.TrimSpace(res.stdout) != "1702" {
		t.Errorf("with 2 MB: exit %d, %q\n%s", res.exit, res.stdout, res.stderr)
	}
	// The same result with room to spare
	if spare := runLumen(t, "run", file); spare.stdout != res.stdout {
		t.Errorf("another result with the default heap: %q", spare.stdout)
	}
}
