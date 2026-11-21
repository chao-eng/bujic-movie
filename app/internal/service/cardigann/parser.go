package cardigann

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// Limits bound what a single definition may consume. They mirror BR-39 and are
// enforced before parsing so a hostile file cannot exhaust memory.
const (
	MaxDefinitionBytes = 2 << 20 // 2MB
	MaxYAMLDepth       = 10
	MaxSettingsFields  = 500
)

// ParseError carries the three-level location required by BR-25: file, block,
// field. Callers surface all three so a user can fix the YAML directly.
type ParseError struct {
	File   string
	Block  string
	Field  string
	Reason string
}

func (e *ParseError) Error() string {
	loc := e.Block
	if e.Field != "" {
		loc = e.Block + "." + e.Field
	}
	if e.File != "" {
		return fmt.Sprintf("%s: %s: %s", e.File, loc, e.Reason)
	}
	return fmt.Sprintf("%s: %s", loc, e.Reason)
}

// Result is the outcome of parsing one definition file.
type Result struct {
	Definition Definition
	Hash       string
	Compat     string   // full / partial / invalid
	Detail     []string // unsupported blocks and validation problems
}

// Parse decodes and validates a Cardigann YAML definition. It never returns a
// nil Definition together with a nil error: an invalid file yields a Definition
// marked invalid plus the reasons.
func Parse(file string, raw []byte) (*Result, error) {
	if len(raw) == 0 {
		return nil, &ParseError{File: file, Block: "top", Reason: "文件为空"}
	}
	if len(raw) > MaxDefinitionBytes {
		return nil, &ParseError{File: file, Block: "top", Reason: fmt.Sprintf("文件超过 %d 字节上限", MaxDefinitionBytes)}
	}
	if depth, bad := estimateDepth(raw); bad {
		return nil, &ParseError{File: file, Block: "top", Field: fmt.Sprintf("深度>%d", MaxYAMLDepth), Reason: "嵌套过深"}
	} else if depth > MaxYAMLDepth {
		return nil, &ParseError{File: file, Block: "top", Field: fmt.Sprintf("深度=%d", depth), Reason: fmt.Sprintf("嵌套超过 %d 层", MaxYAMLDepth)}
	}

	var top map[string]any
	if err := yaml.UnmarshalWithOptions(raw, &top, yaml.UseOrderedMap()); err != nil {
		return nil, &ParseError{File: file, Block: "top", Reason: "YAML 语法错误: " + firstLine(err.Error())}
	}

	var def Definition
	if err := yaml.Unmarshal(raw, &def); err != nil {
		return nil, &ParseError{File: file, Block: "top", Reason: "结构不符: " + firstLine(err.Error())}
	}

	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	regularise(&def, keys)
	// Upstream uses camelCase for requestDelay; accept either spelling.
	if def.RequestDelay == 0 {
		for key, value := range top {
			if strings.EqualFold(key, "requestdelay") {
				switch v := value.(type) {
				case float64:
					def.RequestDelay = v
				case int:
					def.RequestDelay = float64(v)
				case string:
					var f float64
					if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
						def.RequestDelay = f
					}
				}
			}
		}
	}

	sum := sha256.Sum256(raw)
	res := &Result{Definition: def, Hash: hex.EncodeToString(sum[:])}

	if len(def.Settings) > MaxSettingsFields {
		return nil, &ParseError{File: file, Block: "settings", Field: fmt.Sprintf("count=%d", len(def.Settings)), Reason: fmt.Sprintf("字段数超过 %d", MaxSettingsFields)}
	}

	problems := Validate(&def)
	if len(problems) > 0 {
		res.Compat = "invalid"
		res.Detail = problems
		return res, nil
	}

	res.Detail = def.Unsupported
	if len(def.Unsupported) > 0 {
		res.Compat = "partial"
	} else {
		res.Compat = "full"
	}
	return res, nil
}

// unsupportedSet reports which unsupported blocks are actually present, so
// regularise only records real occurrences.
func unsupportedSet(def Definition) map[string]bool {
	set := make(map[string]bool, len(UnsupportedBlocks))
	for _, name := range UnsupportedBlocks {
		set[name] = true
	}
	return set
}

// estimateDepth counts the maximum indentation nesting of block mappings.
func estimateDepth(raw []byte) (int, bool) {
	max := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "\t") {
			return 0, true // tabs are illegal in YAML indentation
		}
		indent := (len(line) - len(trimmed)) / 2
		if indent > max {
			max = indent
		}
		if max > 200 {
			return max, true
		}
	}
	return max, false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ErrNotDefinition marks files that parse as YAML but are not definitions.
var ErrNotDefinition = errors.New("not a cardigann definition")

// CapabilitiesSummary renders the human-readable capability line stored with a
// definition, used by the定义库 list view.
func CapabilitiesSummary(def *Definition) string {
	modes := def.SearchModes()
	if len(modes) == 0 {
		modes = []string{"search"}
	}
	cats := len(def.Caps.Categories) + len(def.Caps.CategoryMappings)
	login := "无登录"
	if def.UsesLogin() {
		login = "需登录"
	}
	if def.NeedsCaptcha() {
		login += "+验证码"
	}
	return fmt.Sprintf("搜索模式 %s；分类 %d；%s", strings.Join(modes, "/"), cats, login)
}
