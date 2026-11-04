package migrations

import "testing"

func TestMigrationVersion(t *testing.T) {
	tests := []struct {
		name    string
		want    int64
		wantErr bool
	}{
		{name: "initial migration", name: "000001_create_wallet_transfer_tables.up.sql", want: 1},
		{name: "second migration", name: "000042_add_example.up.sql", want: 42},
		{name: "missing separator", name: "000001.sql", wantErr: true},
		{name: "non numeric prefix", name: "initial_schema.up.sql", wantErr: true},
		{name: "zero version", name: "000000_initial.up.sql", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := migrationVersion(test.name)
			if (err != nil) != test.wantErr {
				t.Fatalf("migrationVersion(%q) error = %v, wantErr %v", test.name, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("migrationVersion(%q) = %d, want %d", test.name, got, test.want)
			}
		})
	}
}
