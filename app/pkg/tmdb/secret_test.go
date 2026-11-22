package tmdb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TMDB 的 transport 错误自带完整请求 URL，含 api_key。日志与返回给调用方的
// 错误都必须脱敏，否则密钥会出现在日志文件和 API 响应里。
func TestErrorsAndLogsDoNotLeakAPIKey(t *testing.T) {
	const secret = "SECRET_KEY_ABC123"

	// 日志写在 ./data/logs 下，切到临时目录避免污染仓库。
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	// 必然失败的地址，触发 transport 错误（错误文本含完整 URL）。
	c := NewClient(secret, "http://127.0.0.1:1", "zh-CN")
	_, callErr := c.SearchMovie(context.Background(), "x", 0)
	if callErr == nil {
		t.Fatal("expected an error from an unreachable endpoint")
	}
	if strings.Contains(callErr.Error(), secret) {
		t.Errorf("API key leaked in returned error: %v", callErr)
	}

	logPath := filepath.Join(tmp, "data", "logs", "tmdb_api.log")
	raw, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("API key leaked in log file:\n%s", string(raw))
	}
	// 脱敏标记应当出现，证明日志确实写了。
	if !strings.Contains(string(raw), "******") {
		t.Error("log should contain the masked placeholder")
	}
}

// maskSecretText 也要处理百分号编码形式。
func TestMaskSecretText(t *testing.T) {
	c := NewClient("a+b/c=", "", "")
	if got := c.maskSecretText("url?api_key=a+b/c=&x=1"); strings.Contains(got, "a+b/c=") && !strings.Contains(got, "******") {
		t.Errorf("raw key not masked: %q", got)
	}
	// 空 key 不应破坏文本。
	empty := NewClient("", "", "")
	if got := empty.maskSecretText("nothing to mask"); got != "nothing to mask" {
		t.Errorf("empty key changed text: %q", got)
	}
}
