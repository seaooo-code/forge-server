package postgres

import (
	"reflect"
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

func TestInitSchemaMigrationIsRepeatable(t *testing.T) {
	if !isRepeatableMigration("000001_init_schema.sql") {
		t.Fatal("expected init schema migration to be repeatable")
	}
	if isRepeatableMigration("000002_other.sql") {
		t.Fatal("expected regular migration not to be repeatable")
	}
}
