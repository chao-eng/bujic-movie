package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Metadata holds parsed media file metadata.
// Title is the primary name used for TMDB search. It prefers CnName; falls back to EnName.
type Metadata struct {
	Title      string `json:"title"`       // Primary title (computed: CnName > EnName)
	CnName     string `json:"cn_name"`     // Chinese title (if detected)
	EnName     string `json:"en_name"`     // English/Latin title
	Year       int    `json:"year"`
	Season     int    `json:"season"`      // 0 if not TV
	Episodes   []int  `json:"episodes"`    // empty if Movie
	Resolution string `json:"resolution"`  // 1080p, 2160p, 4k, etc.
	Source     string `json:"source"`      // WEB-DL, BluRay, etc.
	Codec      string `json:"codec"`       // h264, h265, hevc, etc.
	IsMovie    bool   `json:"is_movie"`
}

// ---------- compiled regexes ----------

var (
	// Pre-clean: website prefix "www.xxx.org - " or "www.xxx.org    -    "
	prefixRe = regexp.MustCompile(`(?i)^[a-z0-9.\-]+\.(org|com|net|xyz|cc|co|info|cn|me)\s*[-–]\s*`)

	// Pre-clean: file size
	fileSizeRe = regexp.MustCompile(`(?i)[0-9.]+\s*[MGT]i?B\b`)

	// Pre-clean: date patterns "2022.10.14" or "2022-10-14" (valid months/days)
	dateRe = regexp.MustCompile(`\d{4}[\s._-](?:0?[1-9]|1[0-2])[\s._-](?:0?[1-9]|[12]\d|3[01])`)

	// Year range replacement: "2020-2021" -> "2020"
	yearRangeRe = regexp.MustCompile(`([\s.]+)(\d{4})-(\d{4})`)

	// First bracket pattern
	firstBracketRe = regexp.MustCompile(`^[\[【]([^\]】]+)[\]】]`)

	// First bracket check regexes
	bracketDotTitleRe = regexp.MustCompile(`(?i)[A-Za-z]+\..+(?:19|20)\d{2}`)
	bracketResourceRe = regexp.MustCompile(`(?i)(?:2160|1080|720|480)[PIpi]|4K|UHD|Blu[\-.]?ray|REMUX|WEB[\-.]?DL|HDTV`)

	// Token splitter: split by . space () [] 【】 _ - / ~ ; & | # 「 」
	tokenSplitRe = regexp.MustCompile(`[.\s()\[\]【】_\-/～;&#|「」~]+`)

	// Chinese character detector
	chineseRe = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)

	// Season/Episode/Year/Resolution/Source/Codec regexes
	seasonRe      = regexp.MustCompile(`(?i)S(\d{3})|^S(\d{1,3})$|S(\d{1,3})E`)
	episodeRe     = regexp.MustCompile(`(?i)EP?(\d{2,4})$|^EP?(\d{1,4})$|^S\d{1,2}EP?(\d{1,4})$|S\d{2}EP?(\d{2,4})`)
	resolutionRe  = regexp.MustCompile(`(?i)^[SBUHD]*(\d{3,4}[PI]+)|\d{3,4}X(\d{3,4})$`)
	resolutionRe2 = regexp.MustCompile(`(?i)^([248]K)$`)
	sourceRe      = regexp.MustCompile(`(?i)^(BLURAY|HDTV|UHDTV|HDDVD|WEBRIP|DVDRIP|BDRIP|BLU|WEB|BD|HDRIP|REMUX|UHD)$`)
	codecRe       = regexp.MustCompile(`(?i)^(H26[45]|X26[45]|AVC|HEVC|VC\d?|MPEG\d?|XVID|DIVX|AV1|AVS[+23]?)$`)

	// Noise name pattern (to strip from computed Chinese/English names)
	noiseNameRe = regexp.MustCompile(`(?i)PTS|JADE|AOD|CHC|[A-Z]{1,4}TV[\-0-9UVHDK]*|\d{1,2}th|\d{1,2}bit|IMAX|3D|\bXXX\b|\bDC\b|[第共\s]+[0-9一二三四五六七八九十\s\-]+季|[第共\s]+[0-9一二三四五六七八九十百零\s\-]+[集话話]|连载|日剧|美剧|电视剧|动画片|动漫|欧美|西德|日韩|超高清|高清|无水印|下载|蓝光|翡翠台|梦幻天堂·龙网|★?\d*月?新番|最终季|合集|[多中国英葡法俄日韩德意西印泰台港粤双文语简繁体特效内封官译外挂]+字幕|版本|出品|台版|港版|\w+字幕组|\w+字幕社|未删减版|UNCUT$|UNRATE$|WITH EXTRAS$|RERIP$|SUBBED$|PROPER$|REPACK$|SEASON$|EPISODE$|Complete$|Extended$|Extended Version$|S\d{2}\s*-\s*S\d{2}|S\d{2}|\s+S\d{1,2}|EP?\d{2,4}\s*-\s*EP?\d{2,4}|EP?\d{2,4}|\s+EP?\d{1,4}|CD[\s.]*[1-9]|DVD[\s.]*[1-9]|DISK[\s.]*[1-9]|DISC[\s.]*[1-9]|[248]K|\d{3,4}[PIX]+|CD[\s.]*[1-9]|DVD[\s.]*[1-9]|DISK[\s.]*[1-9]|DISC[\s.]*[1-9]|\s+GB`)

	// Chinese number season/episode patterns
	cnSeasonRe  = regexp.MustCompile(`(?i)第\s*([0-9一二三四五六七八九十]+)\s*季`)
	cnEpisodeRe = regexp.MustCompile(`(?i)第\s*([0-9一二三四五六七八九十百零]+)\s*[集话話期幕]`)

	// Name SE words
	nameSeWords = map[string]bool{
		"共": true, "第": true, "季": true, "集": true, "话": true, "話": true, "期": true,
	}

	// Roman numeral pattern
	romanPattern = regexp.MustCompile(`(?i)^(M{1,4}(CM|CD|D?C{0,3})?(XC|XL|L?X{0,3})?(IX|IV|V?I{0,3})?|(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})?(IX|IV|V?I{0,3})?|(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})?|(IX|IV|V?I{0,3}))$`)

	// Known media extensions to safely strip
	mediaExtensions = map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".ts": true,
		".mov": true, ".wmv": true, ".flv": true, ".webm": true,
		".mpeg": true, ".mpg": true, ".m4v": true, ".3gp": true,
		".srt": true, ".ass": true, ".vtt": true, ".sub": true,
	}
)

// ---------- tokenizer ----------

// tokenize splits a filename into tokens by common media-filename delimiters.
func tokenize(text string) []string {
	parts := tokenSplitRe.Split(text, -1)
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			tokens = append(tokens, p)
		}
	}
	return tokens
}

// ---------- Chinese number conversion ----------

var cnNumMap = map[rune]int{
	'零': 0, '一': 1, '二': 2, '三': 3, '四': 4,
	'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	'十': 10, '百': 100,
}

// cnToInt converts a simple Chinese number string to int.
// Handles: 一 → 1, 十二 → 12, 二十 → 20, etc.
func cnToInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	// If it's already a digit string, parse directly
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}

	runes := []rune(s)
	result := 0
	current := 0

	for _, r := range runes {
		v, ok := cnNumMap[r]
		if !ok {
			return 0, false
		}
		if v == 10 {
			if current == 0 {
				current = 1
			}
			result += current * 10
			current = 0
		} else if v == 100 {
			if current == 0 {
				current = 1
			}
			result += current * 100
			current = 0
		} else {
			current = v
		}
	}
	result += current
	return result, result > 0
}

// ---------- Chinese text detection ----------

// containsChinese returns true if the string contains any CJK characters.
func containsChinese(s string) bool {
	return chineseRe.MatchString(s)
}

// ---------- pre-clean ----------

// preClean removes known noise from the filename before tokenization.
func preClean(name string) string {
	// 1. Strip file extension only if it is a known media/subtitle extension
	ext := filepath.Ext(name)
	if mediaExtensions[strings.ToLower(ext)] {
		name = strings.TrimSuffix(name, ext)
	}

	// 2. Strip website prefix: "www.xxx.org - "
	name = prefixRe.ReplaceAllString(name, "")

	// 3. Strip file size patterns
	name = fileSizeRe.ReplaceAllString(name, "")

	// 4. Strip date patterns (but not year-only which we want to keep)
	name = dateRe.ReplaceAllString(name, "")

	// 5. Year range replacement: e.g. "2020-2021" -> "2020"
	name = yearRangeRe.ReplaceAllString(name, "${1}${2}")

	return strings.TrimSpace(name)
}

// ---------- resolution helpers ----------

// normalizeResolutionDim normalizes a resolution dimension to a standard label.
func normalizeResolutionDim(width, height int) string {
	h := height
	if width > height {
		h = height
	}
	switch {
	case h >= 2160:
		return "2160p"
	case h >= 1080:
		return "1080p"
	case h >= 720:
		return "720p"
	case h >= 480:
		return "480p"
	default:
		return strconv.Itoa(h) + "p"
	}
}

// ---------- main parse function ----------

// ParseFilename extracts metadata from a file path or file name.
// Uses a MoviePilot-inspired token pipeline state-machine:
//   Pre-clean → First-bracket check → Tokenize → Sequential classification → Post-process
func ParseFilename(path string) *Metadata {
	filename := filepath.Base(path)

	// --- Stage 1: Pre-clean ---
	cleaned := preClean(filename)

	var (
		cnName         string
		enName         string
		year           int
		beginSeason    = -1
		beginEpisode   = -1
		endEpisode     = -1
		resolution     string
		source         string
		codec          string
		isMovie        = true

		stopName       = false
		stopCnName     = false
		lastToken      = ""
		lastTokenType  = ""
		unknownNameStr = ""
	)

	// --- Stage 1.5: Chinese season/episode extraction ---
	// Extract CJK season/episode before first bracket and token split
	if m := cnSeasonRe.FindStringSubmatch(cleaned); m != nil {
		if n, ok := cnToInt(m[1]); ok {
			beginSeason = n
			isMovie = false
		}
		cleaned = cnSeasonRe.ReplaceAllString(cleaned, " ")
	}
	if m := cnEpisodeRe.FindStringSubmatch(cleaned); m != nil {
		if n, ok := cnToInt(m[1]); ok {
			beginEpisode = n
			isMovie = false
		}
		cleaned = cnEpisodeRe.ReplaceAllString(cleaned, " ")
	}

	// --- Stage 2: Strip first bracket if it's noise ---
	if m := firstBracketRe.FindStringSubmatch(cleaned); m != nil {
		bracketContent := m[1]
		if bracketDotTitleRe.MatchString(bracketContent) && bracketResourceRe.MatchString(bracketContent) {
			// e.g. [Wonder.Woman.1984.2020.BluRay] - keep content but strip brackets
			cleaned = bracketContent + cleaned[len(m[0]):]
		} else {
			// noise bracket like [梦幻天堂·龙网] - completely remove it
			cleaned = cleaned[len(m[0]):]
		}
	}
	cleaned = strings.TrimSpace(cleaned)

	// --- Stage 3: Tokenize ---
	tokens := tokenize(cleaned)

	// --- Stage 4: Token Classification Loop ---
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		upper := strings.ToUpper(token)
		continueFlag := true

		// Reclaim unknownNameStr if present
		if unknownNameStr != "" {
			if cnName == "" {
				if enName == "" {
					enName = unknownNameStr
				} else if unknownNameStr != strconv.Itoa(year) {
					enName = enName + " " + unknownNameStr
				}
				lastTokenType = "enname"
			}
			unknownNameStr = ""
		}

		// Identify Part/CD/Disk (consume and skip as title)
		if (cnName != "" || enName != "") && (year > 0 || beginSeason != -1 || beginEpisode != -1 || resolution != "" || source != "") {
			partPattern := regexp.MustCompile(`(?i)(^PART[0-9ABI]{0,2}$|^CD[0-9]{0,2}$|^DVD[0-9]{0,2}$|^DISK[0-9]{0,2}$|^DISC[0-9]{0,2}$)`)
			if partPattern.MatchString(token) {
				lastTokenType = "part"
				continueFlag = false
				if i+1 < len(tokens) {
					nextVal := tokens[i+1]
					nextUpper := strings.ToUpper(nextVal)
					isDigit := regexp.MustCompile(`^\d{1,2}$`).MatchString(nextVal)
					if isDigit || nextUpper == "A" || nextUpper == "B" || nextUpper == "C" || nextUpper == "I" || nextUpper == "II" || nextUpper == "III" {
						i++ // consume part number
					}
				}
			}
		}

		// Name Parsing logic
		if continueFlag {
			if !stopName {
				if upper == "AKA" {
					continueFlag = false
					stopName = true
				} else if nameSeWords[token] {
					lastTokenType = "name_se_words"
					continueFlag = false
				} else if containsChinese(token) {
					lastTokenType = "cnname"
					if cnName == "" {
						cnName = token
					} else if !stopCnName {
						cnName = cnName + " " + token
						stopCnName = true
					}
					continueFlag = false
				} else {
					// Check digit or Roman numeral
					isRoman := romanPattern.MatchString(token)
					isDigit := regexp.MustCompile(`^\d+$`).MatchString(token)
					if isDigit || isRoman {
						if lastTokenType == "name_se_words" {
							continueFlag = false
						} else if cnName != "" || enName != "" {
							if strings.HasPrefix(token, "0") {
								continueFlag = false // likely episode
							} else if !isRoman && lastTokenType == "cnname" {
								if val, err := strconv.Atoi(token); err == nil && val < 1900 {
									continueFlag = false // likely episode
								}
							}
							
							if continueFlag {
								if (isDigit && len(token) < 4) || isRoman {
									if lastTokenType == "cnname" {
										cnName = cnName + " " + token
									} else if lastTokenType == "enname" {
										enName = enName + " " + token
									}
									continueFlag = false
								} else if isDigit && len(token) == 4 {
									if unknownNameStr == "" {
										unknownNameStr = token
									}
								}
							}
						} else {
							if unknownNameStr == "" {
								unknownNameStr = token
							}
						}
					} else if seasonRe.MatchString(token) {
						stopName = true
					} else if episodeRe.MatchString(token) || sourceRe.MatchString(token) || resolutionRe.MatchString(token) || resolutionRe2.MatchString(token) {
						stopName = true
					} else {
						if enName != "" {
							enName = enName + " " + token
						} else {
							enName = token
						}
						lastTokenType = "enname"
					}
				}
			}
		}

		// Year Parsing logic
		if continueFlag {
			if (cnName != "" || enName != "") && regexp.MustCompile(`^\d{4}$`).MatchString(token) {
				val, _ := strconv.Atoi(token)
				if val > 1900 && val < 2050 {
					if year > 0 {
						if enName != "" {
							enName = enName + " " + strconv.Itoa(year)
						} else if cnName != "" {
							cnName = cnName + " " + strconv.Itoa(year)
						}
					}
					year = val
					lastTokenType = "year"
					continueFlag = false
					stopName = true

					// If enName ends with "season" (case-insensitive), append a space to prevent it from being stripped as noise
					if strings.HasSuffix(strings.ToLower(enName), "season") {
						enName = enName + " "
					}
				}
			}
		}

		// Resolution Parsing logic
		if continueFlag && (cnName != "" || enName != "") {
			if m := resolutionRe.FindStringSubmatch(token); m != nil {
				lastTokenType = "pix"
				continueFlag = false
				stopName = true
				if resolution == "" {
					pix := ""
					for _, val := range m[1:] {
						if val != "" {
							pix = val
							break
						}
					}
					pixLower := strings.ToLower(pix)
					if !strings.HasSuffix(pixLower, "p") && !strings.HasSuffix(pixLower, "i") {
						// e.g. 1920x1080 -> 1080p
						if resolutionDimRe := regexp.MustCompile(`(?i)^(\d{3,4})[xX](\d{3,4})$`); resolutionDimRe.MatchString(pix) {
							sub := resolutionDimRe.FindStringSubmatch(pix)
							w, _ := strconv.Atoi(sub[1])
							h, _ := strconv.Atoi(sub[2])
							pixLower = normalizeResolutionDim(w, h)
						} else {
							pixLower = pixLower + "p"
						}
					}
					resolution = pixLower
				}
			} else if m := resolutionRe2.FindStringSubmatch(token); m != nil {
				lastTokenType = "pix"
				continueFlag = false
				stopName = true
				if resolution == "" {
					resolution = strings.ToLower(m[1])
				}
			}
		}

		// Season Parsing logic
		if continueFlag {
			if m := seasonRe.FindStringSubmatch(token); m != nil {
				lastTokenType = "season"
				isMovie = false
				stopName = true

				if strings.HasSuffix(strings.ToLower(enName), "season") {
					enName = enName + " "
				}

				var seVal string
				for _, val := range m[1:] {
					if val != "" {
						seVal = val
						break
					}
				}
				if seVal != "" {
					if val, err := strconv.Atoi(seVal); err == nil {
						if beginSeason == -1 {
							beginSeason = val
						}
					}
				}
			} else if regexp.MustCompile(`^\d+$`).MatchString(token) {
				if lastTokenType == "SEASON" && beginSeason == -1 && len(token) < 3 {
					val, _ := strconv.Atoi(token)
					beginSeason = val
					lastTokenType = "season"
					stopName = true
					continueFlag = false
					isMovie = false
				}
			} else if strings.EqualFold(token, "SEASON") && beginSeason == -1 {
				lastTokenType = "SEASON"
			}
		}

		// Episode Parsing logic
		if continueFlag {
			if m := episodeRe.FindStringSubmatch(token); m != nil {
				lastTokenType = "episode"
				continueFlag = false
				stopName = true
				isMovie = false
				var epVal string
				for _, val := range m[1:] {
					if val != "" {
						epVal = val
						break
					}
				}
				if epVal != "" {
					if val, err := strconv.Atoi(epVal); err == nil {
						if beginEpisode == -1 {
							beginEpisode = val
						} else if val > beginEpisode {
							endEpisode = val
						}
					}
				}
			} else if regexp.MustCompile(`^\d+$`).MatchString(token) {
				if beginEpisode != -1 && endEpisode == -1 && len(token) < 5 {
					val, _ := strconv.Atoi(token)
					if val > beginEpisode && lastTokenType == "episode" {
						endEpisode = val
						continueFlag = false
						isMovie = false
					}
				} else if beginEpisode == -1 && len(token) > 1 && len(token) < 4 && lastTokenType != "year" && lastTokenType != "videoencode" && token != unknownNameStr {
					val, _ := strconv.Atoi(token)
					beginEpisode = val
					lastTokenType = "episode"
					continueFlag = false
					stopName = true
					isMovie = false
				} else if lastTokenType == "EPISODE" && beginEpisode == -1 && len(token) < 5 {
					val, _ := strconv.Atoi(token)
					beginEpisode = val
					lastTokenType = "episode"
					continueFlag = false
					stopName = true
					isMovie = false
				}
			} else if strings.EqualFold(token, "EPISODE") {
				lastTokenType = "EPISODE"
			}
		}

		// Source/Resource Type Parsing logic
		if continueFlag && (cnName != "" || enName != "") {
			if upper == "DL" && lastTokenType == "source" && lastToken == "WEB" {
				source = "WEB-DL"
				continueFlag = false
			} else if upper == "RAY" && lastTokenType == "source" && lastToken == "BLU" {
				source = "BluRay"
				continueFlag = false
			} else if upper == "WEBDL" {
				source = "WEB-DL"
				continueFlag = false
			} else if upper == "REMUX" && source == "BluRay" {
				source = "BluRay REMUX"
				continueFlag = false
			}

			if continueFlag {
				if m := sourceRe.FindStringSubmatch(token); m != nil {
					lastTokenType = "source"
					continueFlag = false
					stopName = true
					if source == "" {
						source = normalizeSource(m[1])
						lastToken = strings.ToUpper(source)
					}
				}
			}
		}

		// Video Codec/Encode Parsing logic
		if continueFlag && (cnName != "" || enName != "") && (year > 0 || resolution != "" || source != "" || beginSeason != -1 || beginEpisode != -1) {
			if m := codecRe.FindStringSubmatch(token); m != nil {
				continueFlag = false
				stopName = true
				lastTokenType = "videoencode"
				if codec == "" {
					codec = normalizeCodec(m[1])
					lastToken = codec
				} else if codec == "10bit" {
					codec = normalizeCodec(m[1]) + " 10bit"
					lastToken = normalizeCodec(m[1])
				}
			} else if upper == "H" || upper == "X" {
				continueFlag = false
				stopName = true
				lastTokenType = "videoencode"
				lastToken = upper
			} else if (token == "264" || token == "265") && lastTokenType == "videoencode" && (lastToken == "H" || lastToken == "X" || lastToken == "h" || lastToken == "x") {
				codec = strings.ToLower(lastToken + token)
				continueFlag = false
			} else if upper == "10BIT" {
				lastTokenType = "videoencode"
				if codec == "" {
					codec = "10bit"
				} else {
					codec = codec + " 10bit"
				}
				continueFlag = false
			}
		}

		// Bare episode number check when stopName is true
		if stopName && beginEpisode == -1 && len(token) >= 2 && len(token) <= 3 && lastTokenType != "videoencode" {
			if token != "264" && token != "265" {
				if val, err := strconv.Atoi(token); err == nil && val > 0 && val < 2000 {
					beginEpisode = val
					isMovie = false
				}
			}
		}
	}

	// --- Stage 5: Post-process ---

	// Reclaim remaining unknownNameStr
	if unknownNameStr != "" {
		if cnName == "" {
			if enName == "" {
				enName = unknownNameStr
			} else if unknownNameStr != strconv.Itoa(year) {
				enName = enName + " " + unknownNameStr
			}
		}
	}

	// Clean noise strings
	if cnName != "" {
		cnName = noiseNameRe.ReplaceAllString(cnName, "")
		cnName = strings.TrimSpace(cnName)
	}
	if enName != "" {
		enName = noiseNameRe.ReplaceAllString(enName, "")
		enName = strings.TrimSpace(enName)
	}

	// Collapse spaces
	cnName = regexp.MustCompile(`\s+`).ReplaceAllString(cnName, " ")
	enName = regexp.MustCompile(`\s+`).ReplaceAllString(enName, " ")

	// Ensure correct title capitalization
	if enName != "" {
		enName = titleCase(enName)
	}

	// Prefer CJK
	if cnName == "" && containsChinese(enName) {
		cnName = enName
		enName = ""
	}

	// Build primary TMDB Title
	var title string
	if cnName != "" {
		title = cnName
	} else if enName != "" {
		title = enName
	}

	// Build episodes list
	var episodes []int
	if beginEpisode != -1 {
		if endEpisode != -1 && endEpisode > beginEpisode {
			for ep := beginEpisode; ep <= endEpisode; ep++ {
				episodes = append(episodes, ep)
			}
		} else {
			episodes = []int{beginEpisode}
		}
	}

	// Seasons defaults
	season := 0
	if beginSeason != -1 {
		season = beginSeason
	} else if len(episodes) > 0 {
		season = 1
	}

	if season > 0 || len(episodes) > 0 {
		isMovie = false
	}

	return &Metadata{
		Title:      title,
		CnName:     cnName,
		EnName:     enName,
		Year:       year,
		Season:     season,
		Episodes:   episodes,
		Resolution: resolution,
		Source:     source,
		Codec:      codec,
		IsMovie:    isMovie,
	}
}

// titleCase capitalizes the first letter of each word, similar to MoviePilot's str_title.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			runes := []rune(w)
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

// normalizeSource standardizes source names.
func normalizeSource(src string) string {
	switch strings.ToUpper(src) {
	case "WEB-DL", "WEBDL", "WEBRIP":
		return "WEB-DL"
	case "WEB":
		return "WEB"
	case "BLURAY", "BLU":
		return "BluRay"
	case "BDRIP", "BD":
		return "BluRay"
	case "REMUX":
		return "REMUX"
	case "UHD":
		return "UHD"
	case "HDTV", "UHDTV":
		return "HDTV"
	case "DVDRIP":
		return "DVDRip"
	case "HDDVD":
		return "HDDVD"
	default:
		return src
	}
}

// normalizeCodec standardizes codec names.
func normalizeCodec(codec string) string {
	switch strings.ToUpper(codec) {
	case "H264", "X264", "AVC":
		return "h264"
	case "H265", "X265", "HEVC":
		return "h265"
	case "AV1":
		return "av1"
	default:
		return strings.ToLower(codec)
	}
}
