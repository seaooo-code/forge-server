package postgres

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMigrationFiles(t *testing.T) {
	files, err := migrationFiles(fstest.MapFS{
		"000002_second.sql": {Data: []byte("select 2;")},
		"000001_first.sql":  {Data: []byte("select 1;")},
		"README.md":         {Data: []byte("ignore")},
		"nested/file.sql":   {Data: []byte("ignore")},
	})
	if err != nil {
		t.Fatalf("migration files: %v", err)
	}

	expected := []string{"000001_first.sql", "000002_second.sql"}
	if !reflect.DeepEqual(files, expected) {
		t.Fatalf("expected %v, got %v", expected, files)
	}
}

func TestProjectMigrationsAreOrdered(t *testing.T) {
	files, err := migrationFiles(os.DirFS("../../../migrations"))
	if err != nil {
		t.Fatalf("migration files: %v", err)
	}
	if len(files) == 0 || files[0] != "000001_init_schema.sql" {
		t.Fatalf("expected the baseline migration first, got %v", files)
	}
	for i, file := range files {
		if want := fmt.Sprintf("%06d_", i+1); !strings.HasPrefix(file, want) {
			t.Fatalf("migration %q must be numbered consecutively with prefix %q, files: %v", file, want, files)
		}
	}
}
