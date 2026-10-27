package mentions

import "testing"

func TestMentionGrammar(t *testing.T) {
	for _, test := range []struct {
		content    string
		user, role bool
	}{
		{"Привет @Nikita!", true, false}, {"Привет <@42> и <@&admin>", true, true},
		{"@NikitaLong", false, false}, {"mail@Nikita", false, false}, {"https://site/@Nikita", false, false},
		{"\\@Nikita", false, false}, {"`@Nikita` <@&admin>", false, true}, {"```\n<@42>\n```", false, false},
		{"@nikita", false, false}, {"<@9999999999999999999>", false, false}, {"<@0>", false, false},
		{"<@&member>", false, false}, {"😀 @Nikita", true, false}, {"я@Nikita", false, false},
	} {
		targets := Parse(test.content)
		if got := targets.Matches(42, "Nikita", "owner"); got != test.user {
			t.Errorf("%q: user=%v want %v", test.content, got, test.user)
		}
		if got := targets.Matches(9, "Other", "admin"); got != test.role {
			t.Errorf("%q: role=%v want %v", test.content, got, test.role)
		}
	}
	if !Parse("@Никита").Matches(42, "Никита", "member") {
		t.Fatal("unicode name")
	}
	if !Parse("<@42>").Matches(42, "Name With Spaces", "member") {
		t.Fatal("canonical ID")
	}
}
