package main

import "testing"

func TestParseMemoryLimit(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  int64
		ok    bool
	}{
		{"1073741824\n", 1073741824, true}, {"max\n", 0, false}, {"garbage", 0, false}, {"0", 0, false}, {"-1", -1, false}, {"9223372036854775808", 9223372036854775807, false},
	} {
		got, ok := parseMemoryLimit(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("%q: %d %t", tt.input, got, ok)
		}
	}
}
