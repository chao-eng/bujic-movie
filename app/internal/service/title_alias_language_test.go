package service

import "testing"

// A latin site must get the market release title, not a pinyin transliteration
// TMDB stores under the "cn" region.
func TestKeywordForLanguagePrefersMarketTitle(t *testing.T) {
	resolution := &TitleResolution{
		Keyword: "流浪地球",
		Selected: &TitleCandidate{
			TMDBID: 535167, Title: "流浪地球",
			Aliases: []TitleAliasEntry{
				{Title: "流浪地球", Lang: "localized"},
				{Title: "Liú làng dì qiú", Lang: "cn"},
				{Title: "Странстващата Земя", Lang: "bg"},
				{Title: "The Wandering Earth", Lang: "us"},
			},
		},
	}
	if got := KeywordForLanguage(resolution, "流浪地球", "en-US"); got != "The Wandering Earth" {
		t.Errorf("english keyword = %q, want the US release title", got)
	}
	if got := KeywordForLanguage(resolution, "流浪地球", ""); got != "The Wandering Earth" {
		t.Errorf("undeclared language = %q, want the US release title", got)
	}
	if got := KeywordForLanguage(resolution, "流浪地球", "zh-CN"); got != "流浪地球" {
		t.Errorf("chinese keyword = %q, want the original", got)
	}
}

// Korean and Japanese sites get the native-script title.
func TestKeywordForLanguageNativeScript(t *testing.T) {
	resolution := &TitleResolution{
		Keyword: "鱿鱼游戏",
		Selected: &TitleCandidate{
			TMDBID: 93405, Title: "鱿鱼游戏",
			Aliases: []TitleAliasEntry{
				{Title: "鱿鱼游戏", Lang: "localized"},
				{Title: "오징어 게임", Lang: "original"},
				{Title: "Squid Game", Lang: "us"},
				{Title: "イカゲーム", Lang: "jp"},
			},
		},
	}
	if got := KeywordForLanguage(resolution, "鱿鱼游戏", "ko-KR"); got != "오징어 게임" {
		t.Errorf("korean keyword = %q", got)
	}
	if got := KeywordForLanguage(resolution, "鱿鱼游戏", "ja-JP"); got != "イカゲーム" {
		t.Errorf("japanese keyword = %q", got)
	}
}

// Only a transliteration available: use it rather than falling back to Chinese,
// which a latin site cannot answer at all.
func TestKeywordForLanguageFallsBackToTransliteration(t *testing.T) {
	resolution := &TitleResolution{
		Keyword: "三体",
		Selected: &TitleCandidate{
			TMDBID: 204541, Title: "三体",
			Aliases: []TitleAliasEntry{
				{Title: "三体", Lang: "localized"},
				{Title: "San Ti", Lang: "cn"},
			},
		},
	}
	if got := KeywordForLanguage(resolution, "三体", "en"); got != "San Ti" {
		t.Errorf("keyword = %q, want the transliteration as last resort", got)
	}
}

// Japanese and Korean sites must not receive a pinyin transliteration when a
// real market title exists.
func TestKeywordForLanguageNativeFallsBackToMarketTitle(t *testing.T) {
	resolution := &TitleResolution{
		Keyword: "流浪地球",
		Selected: &TitleCandidate{
			Title: "流浪地球",
			Aliases: []TitleAliasEntry{
				{Title: "流浪地球", Lang: "localized"},
				{Title: "Liú làng dì qiú", Lang: "cn"},
				{Title: "The Wandering Earth", Lang: "us"},
			},
		},
	}
	for _, lang := range []string{"ja-JP", "ko-KR"} {
		if got := KeywordForLanguage(resolution, "流浪地球", lang); got != "The Wandering Earth" {
			t.Errorf("%s keyword = %q, want the market title", lang, got)
		}
	}
}

func TestIsTransliteration(t *testing.T) {
	if !isTransliteration("Liú làng dì qiú") {
		t.Error("pinyin with tone marks must count as a transliteration")
	}
	if isTransliteration("The Wandering Earth") {
		t.Error("a plain latin title is not a transliteration")
	}
}
