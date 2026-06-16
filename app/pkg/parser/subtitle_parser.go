package parser

import (
	"path/filepath"
	"regexp"
	"strings"
)

type SubtitleInfo struct {
	Language string `json:"language"` // zh-CN (简中), zh-TW (繁中), ja (日文), en (英文), or unknown
	Format   string `json:"format"`   // srt, ass, ssa, sub, etc.
}

// Order of regexes matters. Matching Traditional Chinese (繁中) MUST happen before
// Simplified Chinese (简中) to prevent "繁中" or "繁体中文" being misidentified as "简中" due to the common "中" suffix.
var (
	traditionalChineseReg = regexp.MustCompile(`(?i)\b(zh-tw|zh-hk|tc|zhtw|zhhk|big5|traditional|繁体|繁體|繁体中文|繁體中文|繁中|繁港|繁)\b`)
	simplifiedChineseReg  = regexp.MustCompile(`(?i)\b(zh-cn|zh|sc|zhcn|simplified|chs|gb|简体|简体中文|简中|中字|中英|中|简)\b`)
	englishReg            = regexp.MustCompile(`(?i)\b(eng?|english|en)\b`)
	japaneseReg           = regexp.MustCompile(`(?i)\b(ja|jap|japanese|日|日文|日语|jp)\b`)
)

// ParseSubtitle extracts language and format from a subtitle file path
func ParseSubtitle(path string) *SubtitleInfo {
	filename := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(filename))
	
	// Strip ext dot
	format := strings.TrimPrefix(ext, ".")
	
	// Convert filename to lowercase for easier matching
	lowerName := strings.ToLower(filename)
	
	// Standardize separators to spaces to enable word boundaries (\b)
	normalized := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(lowerName)

	info := &SubtitleInfo{
		Format:   format,
		Language: "unknown",
	}

	// 1. Check Traditional Chinese first (Priority)
	if traditionalChineseReg.MatchString(normalized) {
		info.Language = "zh-TW"
		return info
	}

	// 2. Check Simplified Chinese
	if simplifiedChineseReg.MatchString(normalized) {
		info.Language = "zh-CN"
		return info
	}

	// 3. Check Japanese
	if japaneseReg.MatchString(normalized) {
		info.Language = "ja"
		return info
	}

	// 4. Check English
	if englishReg.MatchString(normalized) {
		info.Language = "en"
		return info
	}

	return info
}
