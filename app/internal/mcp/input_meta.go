package mcp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// inputMeta accumulates a redacted, tool-specific argument summary for audit
// records (BR-26). It intentionally never stores subtitle content, file bytes,
// or raw media paths.
type inputMeta struct {
	fields map[string]any
}

func newInputMeta() *inputMeta {
	return &inputMeta{fields: make(map[string]any)}
}

func (m *inputMeta) set(key string, val any) {
	m.fields[key] = val
}

// String serializes the redacted meta deterministically (sorted keys).
func (m *inputMeta) String() string {
	if len(m.fields) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m.fields))
	for k := range m.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.Quote(k))
		sb.WriteByte(':')
		b, _ := json.Marshal(m.fields[k])
		sb.Write(b)
	}
	sb.WriteByte('}')
	return sb.String()
}
