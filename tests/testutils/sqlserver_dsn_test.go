package testutils

import "testing"

// Each test gets its own database, which means rewriting the database out of
// the DSN. Doing that wrong sends every test to `master` — they would pass, and
// prove nothing, while dropping each other's tables.
//
// SQL Server cannot be started on an arm64 machine, so this is the part of the
// helper that can still be checked there.
func TestTheDatabaseCanBeSwappedInASQLServerDSN(t *testing.T) {
	for _, c := range []struct{ name, dsn, want string }{
		{
			name: "the form the app itself builds",
			dsn:  "sqlserver://sa:pw@127.0.0.1:1433?database=master&encrypt=disable",
			want: "sqlserver://sa:pw@127.0.0.1:1433?database=gobpm_test_x&encrypt=disable",
		},
		{
			name: "database last, with no parameter after it",
			dsn:  "sqlserver://sa:pw@127.0.0.1:1433?encrypt=disable&database=master",
			want: "sqlserver://sa:pw@127.0.0.1:1433?encrypt=disable&database=gobpm_test_x",
		},
		{
			name: "other parameters but no database",
			dsn:  "sqlserver://sa:pw@127.0.0.1:1433?encrypt=disable",
			want: "sqlserver://sa:pw@127.0.0.1:1433?encrypt=disable&database=gobpm_test_x",
		},
		{
			name: "no parameters at all",
			dsn:  "sqlserver://sa:pw@127.0.0.1:1433",
			want: "sqlserver://sa:pw@127.0.0.1:1433?database=gobpm_test_x",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := sqlServerDSNForDatabase(c.dsn, "gobpm_test_x"); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}
