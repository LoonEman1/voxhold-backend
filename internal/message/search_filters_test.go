package message

import "testing"

func TestParseSearchFilters(t *testing.T) {
	f := ParseSearchFilters("hello from:@alice in:#general has:image before:2026-01-31 after:2026-01-01 world")
	if f.From != "alice" || f.In != "general" || !f.HasImage || f.HasFile || f.HasLink {
		t.Fatalf("filters = %+v", f)
	}
	if f.Before == nil || f.After == nil || f.Before.Day() != 31 || f.After.Day() != 1 {
		t.Fatalf("dates = %v %v", f.Before, f.After)
	}
	if f.Rest != "hello world" {
		t.Fatalf("rest = %q", f.Rest)
	}
	bad := ParseSearchFilters("before:yesterday has:video url:http://x")
	if bad.Any() || bad.Rest != "before:yesterday has:video url:http://x" {
		t.Fatalf("malformed operators must stay text: %+v", bad)
	}
}
