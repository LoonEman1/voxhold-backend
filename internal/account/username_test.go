package account

import "testing"

func TestNormalizeUsername(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"admin", false},
		{"ad min", false},
		{"Иван_Петров", false},
		{"ad  min", true},
		{"a​dmin", true},
		{"adm‮in", true},
		{"adm\nin", true},
		{"ab", true},
		{"bad@name", true},
	}
	for _, c := range cases {
		_, _, err := NormalizeUsername(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err=%v wantErr=%v", c.in, err, c.wantErr)
		}
	}
	_, k1, _ := NormalizeUsername("Admin")
	_, k2, _ := NormalizeUsername("admin")
	if k1 != k2 {
		t.Fatalf("keys differ: %q %q", k1, k2)
	}
}
