package selfupgrade

import "testing"

func TestParseEnvFile(t *testing.T) {
	got, err := ParseEnvFile([]byte(`# the database
DATABASE_URL="postgres://metis:s3cret@db/metis?sslmode=require"

export METIS_HTTP_ADDRESS=:9090
ENCRYPTION_KEY='abc=def'
EMPTY=
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"DATABASE_URL":       "postgres://metis:s3cret@db/metis?sslmode=require",
		"METIS_HTTP_ADDRESS": ":9090",
		"ENCRYPTION_KEY":     "abc=def",
		"EMPTY":              "",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, err := ParseEnvFile([]byte("not a pair\n")); err == nil {
		t.Fatal("a line without = was accepted")
	}
}
