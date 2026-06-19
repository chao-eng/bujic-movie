package sat

import (
	"strings"
	"sync"

	"github.com/longbridgeapp/opencc"
)

var (
	once      sync.Once
	converter *opencc.OpenCC
	initErr   error
)

func initConverter() {
	converter, initErr = opencc.New("t2s")
}

// ToSimplified converts Traditional Chinese characters in the string s to Simplified Chinese using OpenCC.
func ToSimplified(s string) string {
	// Custom pre-convert overrides (e.g. Traditional female '妳' -> general '你')
	s = strings.ReplaceAll(s, "妳", "你")

	once.Do(initConverter)
	if initErr != nil {
		return s
	}
	res, err := converter.Convert(s)
	if err != nil {
		return s
	}

	// Double check post-convert to clean any remaining '妳'
	res = strings.ReplaceAll(res, "妳", "你")
	return res
}
