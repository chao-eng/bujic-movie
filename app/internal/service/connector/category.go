package connector

import "fmt"

// newznabCategoryNames maps a Newznab category id to a short Chinese label.
// The UI shows these instead of raw numbers so a result reads as "剧集/HD"
// rather than "208".
var newznabCategoryNames = map[int]string{
	1000: "主机游戏", 1010: "主机游戏/PS4", 1030: "主机游戏/Switch", 1080: "主机游戏/PS4",
	2000: "电影", 2010: "电影/外语", 2030: "电影/SD", 2040: "电影/HD",
	2045: "电影/4K", 2050: "电影/蓝光", 2060: "电影/3D", 2070: "电影/剧照",
	3000: "音乐", 3010: "音乐/MP3", 3040: "音乐/无损",
	4000: "软件", 4030: "软件/Mac", 4050: "软件/游戏",
	5000: "剧集", 5020: "剧集/外语", 5030: "剧集/SD", 5040: "剧集/HD",
	5045: "剧集/4K", 5060: "剧集/体育", 5070: "剧集/动画", 5080: "剧集/纪录片",
	6000: "成人",
	7000: "书籍", 7020: "书籍/电子书", 7030: "书籍/漫画", 7040: "书籍/杂志",
	8000: "其它",
}

// CategoryName returns a human-readable label for a Newznab category id.
// Unknown ids fall back to the closest known parent, then to "其它".
func CategoryName(id int) string {
	if name, ok := newznabCategoryNames[id]; ok {
		return name
	}
	for level := 100; level <= 1000; level *= 10 {
		parent := id / level * level
		if name, ok := newznabCategoryNames[parent]; ok {
			return name
		}
	}
	if id > 0 {
		return fmt.Sprintf("其它（%d）", id)
	}
	return "其它"
}

// CategoryNames labels every category and drops duplicates, keeping order.
func CategoryNames(ids []int) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name := CategoryName(id)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// primaryCategory picks the most specific category for display.
func primaryCategory(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	best := ids[0]
	for _, id := range ids {
		if id > best {
			best = id
		}
	}
	return CategoryName(best)
}
