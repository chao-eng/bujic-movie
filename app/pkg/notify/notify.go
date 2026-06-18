// Package notify provides thin clients for third-party message notification
// channels (企业微信/钉钉/飞书 群机器人, Server酱, Bark, PushPlus, PushDeer, Gotify,
// ntfy, and a generic custom Webhook). It is modeled on the MoviePilot
// "mergemessagenotify" plugin. Each channel is configured by a flat map of
// string key/values; the per-type field schema is exposed via Types() so the
// frontend can render the form dynamically.
package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Message is a notification to deliver.
type Message struct {
	Title string
	Text  string
	Link  string // optional click-through URL
	Image string // optional image/poster URL
}

// Channel sends messages to one configured destination.
type Channel interface {
	Send(ctx context.Context, msg Message) error
}

// Field describes one config input for a channel type (drives the frontend form).
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`               // "text" | "password" | "textarea"
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
	Default     string `json:"default,omitempty"`
}

// ChannelType is the schema for one channel kind.
type ChannelType struct {
	Type   string  `json:"type"`
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

// Types returns the schema for every supported channel type.
func Types() []ChannelType {
	return []ChannelType{
		{Type: "wecombot", Name: "企业微信机器人", Fields: []Field{
			{Key: "key", Label: "机器人 Key", Type: "text", Required: true, Placeholder: "webhook 地址中的 key 参数"},
		}},
		{Type: "dingtalk", Name: "钉钉机器人", Fields: []Field{
			{Key: "access_token", Label: "Access Token", Type: "text", Required: true},
			{Key: "secret", Label: "加签密钥（可选）", Type: "text", Placeholder: "SEC 开头，安全设置为加签时填写"},
		}},
		{Type: "feishu", Name: "飞书机器人", Fields: []Field{
			{Key: "access_token", Label: "Webhook Token", Type: "text", Required: true, Placeholder: "hook 地址最后一段"},
			{Key: "secret", Label: "加签密钥（可选）", Type: "text"},
		}},
		{Type: "serverchan", Name: "Server酱", Fields: []Field{
			{Key: "send_key", Label: "SendKey", Type: "text", Required: true},
		}},
		{Type: "serverchan3", Name: "Server酱 3", Fields: []Field{
			{Key: "uid", Label: "UID", Type: "text", Required: true, Placeholder: "SCT 后台“我的 UID”"},
			{Key: "send_key", Label: "SendKey", Type: "password", Required: true, Placeholder: "SCT 后台“消息发送 Key”"},
		}},
		{Type: "bark", Name: "Bark", Fields: []Field{
			{Key: "server_url", Label: "服务器地址", Type: "text", Default: "https://api.day.app", Placeholder: "https://api.day.app"},
			{Key: "push_key", Label: "推送 Key", Type: "text", Required: true},
			{Key: "group", Label: "分组（可选）", Type: "text"},
			{Key: "sound", Label: "铃声（可选）", Type: "text"},
		}},
		{Type: "pushplus", Name: "PushPlus", Fields: []Field{
			{Key: "token", Label: "用户令牌", Type: "text", Required: true},
			{Key: "topic", Label: "群组编码（可选）", Type: "text"},
		}},
		{Type: "pushdeer", Name: "PushDeer", Fields: []Field{
			{Key: "server_url", Label: "服务器地址", Type: "text", Default: "https://api2.pushdeer.com", Placeholder: "https://api2.pushdeer.com"},
			{Key: "push_key", Label: "PushKey", Type: "text", Required: true},
		}},
		{Type: "gotify", Name: "Gotify", Fields: []Field{
			{Key: "server_url", Label: "服务器地址", Type: "text", Required: true, Placeholder: "http://127.0.0.1:8080"},
			{Key: "token", Label: "应用 Token", Type: "text", Required: true},
		}},
		{Type: "ntfy", Name: "ntfy", Fields: []Field{
			{Key: "server_url", Label: "服务器地址", Type: "text", Default: "https://ntfy.sh", Placeholder: "https://ntfy.sh"},
			{Key: "topic", Label: "主题 Topic", Type: "text", Required: true},
			{Key: "token", Label: "访问令牌（可选）", Type: "text", Placeholder: "tk_xxx，与用户名密码二选一"},
			{Key: "username", Label: "用户名（可选）", Type: "text"},
			{Key: "password", Label: "密码（可选）", Type: "password"},
		}},
		{Type: "webhook", Name: "自定义 Webhook", Fields: []Field{
			{Key: "url", Label: "请求 URL", Type: "text", Required: true, Placeholder: "支持模板变量 {title} {text}"},
			{Key: "method", Label: "请求方法", Type: "text", Default: "POST", Placeholder: "GET / POST / PUT"},
			{Key: "content_type", Label: "Content-Type（可选）", Type: "text", Placeholder: "application/json"},
			{Key: "body", Label: "请求体模板（可选）", Type: "textarea", Placeholder: `如 {"title":"{title}","body":"{text}"}`},
		}},
	}
}

// New builds a Channel for the given type from a flat config map.
func New(channelType string, config map[string]string) (Channel, error) {
	base := &baseClient{http: &http.Client{Timeout: 15 * time.Second}}
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case "wecombot":
		return &wecomBot{base: base, cfg: config}, nil
	case "dingtalk":
		return &dingtalkBot{base: base, cfg: config}, nil
	case "feishu":
		return &feishuBot{base: base, cfg: config}, nil
	case "serverchan":
		return &serverChan{base: base, cfg: config}, nil
	case "serverchan3":
		return &serverChan3{base: base, cfg: config}, nil
	case "bark":
		return &bark{base: base, cfg: config}, nil
	case "pushplus":
		return &pushPlus{base: base, cfg: config}, nil
	case "pushdeer":
		return &pushDeer{base: base, cfg: config}, nil
	case "gotify":
		return &gotify{base: base, cfg: config}, nil
	case "ntfy":
		return &ntfy{base: base, cfg: config}, nil
	case "webhook":
		return &webhook{base: base, cfg: config}, nil
	default:
		return nil, fmt.Errorf("不支持的通知渠道类型: %s", channelType)
	}
}

// ---- shared helpers ----

type baseClient struct {
	http *http.Client
}

func get(cfg map[string]string, key string) string {
	return strings.TrimSpace(cfg[key])
}

// do performs an HTTP request and returns the body + status code.
func (c *baseClient) do(ctx context.Context, method, urlStr string, headers map[string]string, body []byte) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, reader)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	dur := time.Since(start)
	if err != nil {
		logRequest(method, urlStr, 0, dur, err)
		return nil, 0, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	logRequest(method, urlStr, resp.StatusCode, dur, nil)
	return respBody, resp.StatusCode, nil
}

func (c *baseClient) postJSON(ctx context.Context, urlStr string, body []byte) ([]byte, int, error) {
	return c.do(ctx, "POST", urlStr, map[string]string{"Content-Type": "application/json"}, body)
}

func logRequest(method, urlStr string, statusCode int, duration time.Duration, reqErr error) {
	logFilePath := "data/logs/notify_api.log"
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	masked := maskQuery(urlStr)
	var line string
	if reqErr != nil {
		line = fmt.Sprintf("[%s] %s %s | ERROR: %v\n", ts, method, masked, reqErr)
	} else {
		line = fmt.Sprintf("[%s] %s %s | STATUS: %d | DURATION: %v\n", ts, method, masked, statusCode, duration)
	}
	_, _ = f.WriteString(line)
}

// maskQuery hides secret-bearing query params in logs.
func maskQuery(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr
	}
	q := u.Query()
	for _, key := range []string{"key", "access_token", "token", "sign", "X-Plex-Token"} {
		if q.Get(key) != "" {
			q.Set(key, "******")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// renderTemplate replaces {title} {text} {link} {image} placeholders.
func renderTemplate(tmpl string, msg Message) string {
	r := strings.NewReplacer(
		"{title}", msg.Title,
		"{text}", msg.Text,
		"{link}", msg.Link,
		"{image}", msg.Image,
	)
	return r.Replace(tmpl)
}
