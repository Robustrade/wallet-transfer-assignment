package migrations

import "testing"

func TestMigrationVersion(t *testing.T) {
	tests := []struct {
		label    string
		filename string
		want     int64
		wantErr  bool
	}{
		{label: "initial migration", filename: "000001_create_wallet_transfer_tables.up.sql", want: 1},
		{label: "second migration", filename: "000042_add_example.up.sql", want: 42},
		{label: "missing separator", filename: "000001.sql", wantErr: true},
		{label: "non numeric prefix", filename: "initial_schema.up.sql", wantErr: true},
		{label: "zero version", filename: "000000_initial.up.sql", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			got, err := migrationVersion(test.filename)
			if (err != nil) != test.wantErr {
				t.Fatalf("migrationVersion(%q) error = %v, wantErr %v", test.filename, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("migrationVersion(%q) = %d, want %d", test.filename, got, test.want)
			}
		})
	}
}
