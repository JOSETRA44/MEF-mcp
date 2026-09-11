package store

import "testing"

func TestColumnTypeKeepsLeadingZeroCodesAsText(t *testing.T) {
	cases := []struct {
		values []string
		want   string
	}{
		{[]string{"1", "22", "-3"}, "INTEGER"},
		{[]string{"1", "2.5", ""}, "REAL"},
		{[]string{"0040", "0068"}, "TEXT"}, // programa presupuestal
		{[]string{"13", "01"}, "TEXT"},     // one zero-padded sector poisons INTEGER
		{[]string{"0", "0.5"}, "REAL"},     // "0" and "0.5" are not codes
		{[]string{"", " "}, "TEXT"},        // nothing to infer from
		{[]string{"LIMA"}, "TEXT"},
		{[]string{"0x1F"}, "TEXT"},
		{[]string{"12abc"}, "TEXT"},
		{[]string{"1234567890123456789"}, "TEXT"}, // beyond int64-safe digits
	}
	for _, c := range cases {
		if got := ColumnType(c.values); got != c.want {
			t.Errorf("%q: got %s want %s", c.values, got, c.want)
		}
	}
}

func TestValidTable(t *testing.T) {
	for _, ok := range []string{"gasto", "canon_2025", "_x"} {
		if err := ValidTable(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1abc", "a-b", "drop table x", "sqlite_master", "_meta_downloads", `x"y`} {
		if ValidTable(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestQuoteEscapesIdentifiers(t *testing.T) {
	if got := Quote(`a"b`); got != `"a""b"` {
		t.Fatalf("got %s", got)
	}
}
