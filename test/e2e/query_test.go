package e2e

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The query engine of examples/query is the second real program written
// in Lumen that the tests run: SELECT with joins, conditions, groups and
// sorting over tables kept in CSV files, some 1,100 lines in five
// modules. Where the Lox interpreter walks a tree, this one moves data:
// thousands of rows of numbers and texts, looked up, grouped, sorted.
//
// What is right is what SQLite answers. test/e2e/query/queries holds fifty
// queries, each with SQLite's answer next to it (generate.py makes both
// the tables and the answers); the engine has to give the same, to the
// last digit.

const queryFolder = "test/e2e/query"

func queryFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, queryFolder, "queries", "*.sql"))
	if err != nil || len(files) < 40 {
		t.Fatalf("expected the queries, found %d: %v", len(files), err)
	}
	return files
}

func buildQuery(t *testing.T) string {
	t.Helper()
	compiled := filepath.Join(t.TempDir(), "query.lc")
	if res := runLumen(t, "build", "examples/query", compiled); res.exit != 0 {
		t.Fatalf("the query engine does not compile:\n%s", res.stderr)
	}
	return compiled
}

func TestQueries(t *testing.T) {
	compiled := buildQuery(t)
	data := filepath.Join(queryFolder, "data")

	for _, file := range queryFiles(t) {
		name := strings.TrimSuffix(filepath.Base(file), ".sql")
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(strings.TrimSuffix(file, ".sql") + ".expected")
			if err != nil {
				t.Fatalf("no answer of SQLite to compare with (run generate.py): %v", err)
			}
			res := runLumen(t, "run", compiled, data, filepath.Join(queryFolder, "queries", name+".sql"))
			if res.exit != 0 || res.stderr != "" {
				t.Fatalf("exit %d\n%s", res.exit, res.stderr)
			}
			if res.stdout != string(want) {
				t.Errorf("the engine answers\n%s\nSQLite answers\n%s", firstLines(res.stdout, 12), firstLines(string(want), 12))
			}
		})
	}
}

// firstLines keeps a failure readable: where two answers differ shows early.
func firstLines(text string, count int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > count {
		return strings.Join(lines[:count], "\n") + fmt.Sprintf("\n... (%d lines)", len(lines))
	}
	return text
}

// TestQueryAnswersAreSQLites asks SQLite again, where it is installed, and
// checks that the answers kept next to the queries are still its own: the
// engine is compared with SQLite, not with what someone typed.
func TestQueryAnswersAreSQLites(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed: the answers kept with the queries are taken as they are")
	}

	// The tables, as SQLite is given them: the first line of each file
	// names its columns and says what they hold
	kinds := map[string]string{"int": "INTEGER", "real": "REAL", "text": "TEXT"}
	var setup strings.Builder
	setup.WriteString("BEGIN;\n")
	tables, _ := filepath.Glob(filepath.Join(repoRoot, queryFolder, "data", "*.csv"))
	for _, file := range tables {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(f).ReadAll()
		f.Close()
		if err != nil || len(rows) < 2 {
			t.Fatalf("%s: %v", file, err)
		}
		table := strings.TrimSuffix(filepath.Base(file), ".csv")
		var columns []string
		var text []bool
		for _, title := range rows[0] {
			name, kind, _ := strings.Cut(title, ":")
			columns = append(columns, name+" "+kinds[kind])
			text = append(text, kind == "text")
		}
		fmt.Fprintf(&setup, "CREATE TABLE %s (%s);\n", table, strings.Join(columns, ", "))
		for _, row := range rows[1:] {
			values := make([]string, len(row))
			for i, cell := range row {
				values[i] = cell
				if text[i] {
					values[i] = "'" + strings.ReplaceAll(cell, "'", "''") + "'"
				}
			}
			fmt.Fprintf(&setup, "INSERT INTO %s VALUES (%s);\n", table, strings.Join(values, ", "))
		}
	}
	setup.WriteString("COMMIT;\n")

	database := filepath.Join(t.TempDir(), "oracle.db")
	load := exec.Command(sqlite, database)
	load.Stdin = strings.NewReader(setup.String())
	if out, err := load.CombinedOutput(); err != nil {
		t.Fatalf("SQLite does not take the tables: %v\n%s", err, out)
	}

	for _, file := range queryFiles(t) {
		sql, _ := os.ReadFile(file)
		ask := exec.Command(sqlite, "-separator", "|", "-nullvalue", "NULL", database)
		ask.Stdin = strings.NewReader(string(sql))
		out, err := ask.CombinedOutput()
		if err != nil {
			t.Errorf("%s: SQLite refuses it: %v\n%s", filepath.Base(file), err, out)
			continue
		}
		kept, _ := os.ReadFile(strings.TrimSuffix(file, ".sql") + ".expected")
		if string(out) != string(kept) {
			t.Errorf("%s: the answer kept is not what SQLite answers now; run generate.py", filepath.Base(file))
		}
	}
}

// A query that cannot be answered says why, and the program ends with 1.
func TestQueryErrors(t *testing.T) {
	compiled := buildQuery(t)
	data := filepath.Join(queryFolder, "data")

	cases := []struct {
		name, query, want string
	}{
		{"a column no table has", "SELECT salary FROM customers", "there is no column salary"},
		{"a column of a table that has none such", "SELECT c.price FROM customers c", "there is no column c.price"},
		{"a column two tables have", "SELECT id FROM orders JOIN products ON products.id = product_id", "the name id is of several tables"},
		{"a table that is not there", "SELECT * FROM invoices", "there is no table invoices"},
		{"a function that is not there", "SELECT MEDIAN(price) FROM products", "there is no function MEDIAN"},
		{"too many values for a function", "SELECT UPPER(name, city) FROM customers", "UPPER does not take 2 values"},
		{"a sum asked of one row", "SELECT id FROM orders WHERE SUM(quantity) > 3", "cannot be asked of one row"},
		{"a count in a join", "SELECT o.id FROM orders o JOIN products p ON COUNT(*) = p.id", "cannot be asked of one row"},
		{"nothing after FROM", "SELECT id FROM", "expected the name of a table"},
		{"a comma too many", "SELECT id, FROM orders", "expected a value, found 'FROM'"},
		{"a text not closed", "SELECT 'oops FROM orders", "is not closed"},
		{"a character that means nothing", "SELECT id FROM orders WHERE id = #3", "did not expect '#'"},
		{"something after the query", "SELECT id FROM orders LIMIT 3 4", "did not expect '4' after the query"},
		{"NOT without what it negates", "SELECT id FROM orders WHERE id NOT 3", "expected IN, LIKE or BETWEEN after NOT"},
		{"a parenthesis left open", "SELECT (id + 1 FROM orders", "expected )"},
		{"a limit that is not a number", "SELECT id FROM orders LIMIT many", "expected a whole number"},
		{"CASE without WHEN", "SELECT CASE id END FROM orders", "expected WHEN after CASE"},
		{"not a query", "DELETE FROM orders", "expected SELECT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := runLumen(t, "run", compiled, data, c.query)
			if res.exit != 1 || res.stdout != "" {
				t.Errorf("exit %d, wrote %q", res.exit, res.stdout)
			}
			if !strings.HasPrefix(res.stderr, "Error: ") || !strings.Contains(res.stderr, c.want) {
				t.Errorf("expected an error with %q, got:\n%s", c.want, res.stderr)
			}
		})
	}

	// A table whose file is not well formed
	broken := project(t, map[string]string{
		"short.csv": "id:int,name:text\n1,one\n2\n",
		"empty.csv": "",
	})
	for query, want := range map[string]string{
		"SELECT * FROM short": "line 3 of the table short has 1 values for 2 columns",
		"SELECT * FROM empty": "the table empty is empty",
	} {
		if res := runLumen(t, "run", compiled, broken, query); res.exit != 1 || !strings.Contains(res.stderr, want) {
			t.Errorf("%s: expected %q, got exit %d\n%s", query, want, res.exit, res.stderr)
		}
	}

	// And the right number of arguments
	if res := runLumen(t, "run", compiled, data); res.exit != 2 || !strings.Contains(res.stderr, "Usage:") {
		t.Errorf("with one argument: exit %d\n%s", res.exit, res.stderr)
	}
}
