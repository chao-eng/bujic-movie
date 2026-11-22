package service

import "testing"

// A spin-off whose title merely contains the query must never win over the
// original show. Both cases below were observed against real TMDB data.
func TestMatchRankPrefersExactTitle(t *testing.T) {
	candidates := []TitleCandidate{
		{
			TMDBID: 220080, Title: "鱿鱼游戏马来版", VoteCount: 5,
			Aliases: []TitleAliasEntry{{Title: "鱿鱼游戏马来版", Lang: "localized"}},
		},
		{
			TMDBID: 93405, Title: "鱿鱼游戏", VoteCount: 16000,
			Aliases: []TitleAliasEntry{
				{Title: "鱿鱼游戏", Lang: "localized"},
				{Title: "오징어 게임", Lang: "original"},
				{Title: "Squid Game", Lang: "us"},
			},
		},
		{
			TMDBID: 204082, Title: "鱿鱼游戏：真人挑战赛", VoteCount: 1100,
			Aliases: []TitleAliasEntry{{Title: "鱿鱼游戏：真人挑战赛", Lang: "localized"}},
		},
	}
	for i := range candidates {
		candidates[i].MatchRank = matchRank(candidates[i], "鱿鱼游戏")
	}
	sortCandidates(candidates)
	selected := pickCandidate(candidates, "鱿鱼游戏")
	if selected == nil || selected.TMDBID != 93405 {
		t.Fatalf("selected = %+v, want the original show", selected)
	}
	if candidates[0].TMDBID != 93405 {
		t.Errorf("the list must lead with the exact match, got %+v", candidates[0])
	}
}

// A special edition titled "流浪地球：飞跃2020特别版" must not beat the film
// whose title is exactly the query.
func TestMatchRankPrefersBaseTitle(t *testing.T) {
	special := TitleCandidate{
		TMDBID: 1381967, Title: "流浪地球：飞跃2020特别版", VoteCount: 900,
		Aliases: []TitleAliasEntry{{Title: "流浪地球：飞跃2020特别版", Lang: "localized"}},
	}
	base := TitleCandidate{
		TMDBID: 535167, Title: "流浪地球", VoteCount: 4200,
		Aliases: []TitleAliasEntry{
			{Title: "流浪地球", Lang: "localized"},
			{Title: "The Wandering Earth", Lang: "us"},
		},
	}
	special.MatchRank = matchRank(special, "流浪地球")
	base.MatchRank = matchRank(base, "流浪地球")
	if base.MatchRank <= special.MatchRank {
		t.Fatalf("base rank %d must beat special edition rank %d", base.MatchRank, special.MatchRank)
	}
}

// Typing the season along with the name must still match the show itself.
func TestMatchRankIgnoresSeasonSuffix(t *testing.T) {
	show := TitleCandidate{
		TMDBID: 93405, Title: "鱿鱼游戏",
		Aliases: []TitleAliasEntry{{Title: "鱿鱼游戏", Lang: "localized"}},
	}
	if got := matchRank(show, "鱿鱼游戏 第二季"); got != 100 {
		t.Errorf("rank = %d, want 100 for the base title", got)
	}
	if got := stripSeasonSuffix("鱿鱼游戏 S02"); got != "鱿鱼游戏" {
		t.Errorf("stripSeasonSuffix = %q", got)
	}
}

// An unrelated title scores zero so it can never be selected.
func TestMatchRankRejectsUnrelated(t *testing.T) {
	other := TitleCandidate{Title: "Oppenheimer", Aliases: []TitleAliasEntry{{Title: "Oppenheimer"}}}
	if got := matchRank(other, "鱿鱼游戏"); got != 0 {
		t.Errorf("rank = %d, want 0", got)
	}
}
