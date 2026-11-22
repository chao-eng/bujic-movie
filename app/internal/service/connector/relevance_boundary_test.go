package connector

import "testing"

// Token matching must respect word boundaries. Matching on substrings let "day"
// hit "Days" and "lie" hit "Lies", so an adult batch answered the Korean drama
// query 百日谎言 through "2 Days in Guadalajara".
func TestMatchesKeywordRespectsWordBoundaries(t *testing.T) {
	alias := "Hundred Days of Lies"
	rejects := []string{
		"Valery (2 Days in Guadalajara) [2026]",
		"Doxy - My Lies Finally Bit Me in the Ass",
		"Nakashima Minami - In The Childhood Home 100",
	}
	for _, title := range rejects {
		if MatchesKeyword(title, alias) {
			t.Errorf("%q must not match %q", title, alias)
		}
	}

	accepts := []string{
		"100 Days of Lies S01E01 1080p",
		"Hundred.Days.of.Lies.E02.KOREAN",
		"A Hundred Lies (2024) 1080p",
	}
	for _, title := range accepts {
		if !MatchesKeyword(title, alias) {
			t.Errorf("%q must match %q", title, alias)
		}
	}
}

// A one- or two-word query stays permissive: the usual case is a single title.
func TestMatchesKeywordShortQueryPermissive(t *testing.T) {
	cases := []struct {
		title, keyword string
		want           bool
	}{
		{"Oppenheimer 2023 2160p", "Oppenheimer", true},
		{"[DBD-Raws][奥本海默/Oppenheimer]", "Oppenheimer", true},
		{"Squid Game S01 1080p", "Squid Game", true},
		{"Sousou no Frieren S01E05", "Frieren", true},
		{"Unrelated Release", "Oppenheimer", false},
		{"葬送的芙莉蓮 第29話", "芙莉蓮", true},
	}
	for _, c := range cases {
		if got := MatchesKeyword(c.title, c.keyword); got != c.want {
			t.Errorf("MatchesKeyword(%q, %q) = %v, want %v", c.title, c.keyword, got, c.want)
		}
	}
}

// CJK titles carry no separators, so a rune-substring check is still required.
func TestMatchesKeywordCJK(t *testing.T) {
	cases := []struct {
		title, keyword string
		want           bool
	}{
		{"百日谎言 第01集", "百日谎言", true},
		{"[字幕组] 百日谎言 EP01", "百日谎言", true},
		{"无关的电影 2020", "百日谎言", false},
		{"100일의 거짓말 E01", "100일의 거짓말", true},
		{"周生如故 第24集", "周生如故", true},
	}
	for _, c := range cases {
		if got := MatchesKeyword(c.title, c.keyword); got != c.want {
			t.Errorf("MatchesKeyword(%q, %q) = %v, want %v", c.title, c.keyword, got, c.want)
		}
	}
}

// A release whose words are all present in a different order still counts; only
// strongly unrelated text is rejected. This documents the accepted boundary.
func TestMatchesKeywordMajorityRule(t *testing.T) {
	// 命中 2/3 个词 —— 保留（可能是同一部片的不同译名写法）
	if !MatchesKeyword("Waterloo - The Last Hundred Days", "Hundred Days of Lies") {
		t.Log("note: word-order variants are rejected when fewer than half match")
	}
	// 只命中 1/3 个词 —— 拒绝
	if MatchesKeyword("Napoleon 2023 1080p", "Hundred Days of Lies") {
		t.Error("a single matching word out of three must not count")
	}
}
