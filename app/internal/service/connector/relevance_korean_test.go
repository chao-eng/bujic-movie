package connector

import "testing"

// 百日谎言（韩剧，原名 100일의 거짓말）走的是 TMDB 别名解析。它的英文别名
// "100 Days of Lies" 带数字前缀，而 "100" 出现在大量无关资源名里
// （集数、年份、JAV 编号），曾经让一整批无关资源被判为「相关」。
func TestKoreanDramaWithNumericPrefixIsGuarded(t *testing.T) {
	aliases := []string{
		"百日谎言",
		"100일의 거짓말",
		"百日的谎言",
		"100ilui Geojitmal",
		"100-il-ui Geo-jit-mal",
		"Baekilui Geojinmal",
		"Hundred Days of Lies",
		"Hundred Days of Deception",
		"100 Days of Lies",
	}

	// 一批完全不相关的条目，标题里恰好带了 "100"。
	javBatch := make([]Release, 0, 12)
	for i := 0; i < 12; i++ {
		javBatch = append(javBatch, Release{
			Title: "Nakashima Minami - In The Childhood Home Where Everyone Else 100",
		})
	}
	if kept, dropped := DropIrrelevantBatchFor(javBatch, aliases); !dropped || len(kept) != 0 {
		t.Fatalf("numeric prefix must not rescue an unrelated batch: dropped=%v kept=%d", dropped, len(kept))
	}

	// 真正答复合名的资源必须保留。
	real := []Release{
		{Title: "100 Days of Lies S01E01 1080p WEB-DL"},
		{Title: "Hundred.Days.of.Lies.2024.KOREAN.1080p.WEBRip"},
	}
	if kept, dropped := DropIrrelevantBatchFor(real, aliases); dropped || len(kept) != 2 {
		t.Fatalf("the real releases must be kept: dropped=%v kept=%d", dropped, len(kept))
	}
}

// 韩国站点会收到韩文原名，中文站收到中文名，两者都必须能命中。
func TestKoreanDramaMatchesLocalizedAliases(t *testing.T) {
	cases := []struct {
		title string
		alias string
		want  bool
	}{
		{"100일의 거짓말 E01 1080p", "100일의 거짓말", true},
		{"百日谎言 第01集", "百日谎言", true},
		{"Baekilui Geojinmal S01E01", "Baekilui Geojinmal", true},
		{"Completely Unrelated Movie 2020", "百日谎言", false},
	}
	for _, c := range cases {
		if got := MatchesKeyword(c.title, c.alias); got != c.want {
			t.Errorf("MatchesKeyword(%q, %q) = %v, want %v", c.title, c.alias, got, c.want)
		}
	}
}
