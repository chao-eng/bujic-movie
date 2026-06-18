package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ---- Gotify ----

type gotify struct {
	base *baseClient
	cfg  map[string]string
}

func (c *gotify) Send(ctx context.Context, msg Message) error {
	serverURL := strings.TrimRight(get(c.cfg, "server_url"), "/")
	token := get(c.cfg, "token")
	if serverURL == "" || token == "" {
		return fmt.Errorf("缺少服务器地址或 Token")
	}
	message := msg.Text
	if message == "" {
		message = msg.Title
	}
	payload, _ := json.Marshal(map[string]any{
		"title":    titleOr(msg),
		"message":  message,
		"priority": 5,
	})
	body, status, err := c.base.postJSON(ctx, serverURL+"/message?token="+url.QueryEscape(token), payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	var r struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"errorDescription"`
	}
	_ = json.Unmarshal(body, &r)
	if r.Error != "" {
		return fmt.Errorf("发送失败: %s %s", r.Error, r.ErrorDescription)
	}
	return nil
}

// ---- ntfy ----

type ntfy struct {
	base *baseClient
	cfg  map[string]string
}

func (c *ntfy) Send(ctx context.Context, msg Message) error {
	serverURL := strings.TrimRight(get(c.cfg, "server_url"), "/")
	if serverURL == "" {
		serverURL = "https://ntfy.sh"
	}
	topic := get(c.cfg, "topic")
	if topic == "" {
		return fmt.Errorf("缺少主题 Topic")
	}
	headers := map[string]string{}
	if msg.Title != "" {
		headers["X-Title"] = msg.Title
	}
	if msg.Link != "" {
		headers["X-Click"] = msg.Link
	}
	if token := get(c.cfg, "token"); token != "" {
		headers["Authorization"] = "Bearer " + token
	} else if u, p := get(c.cfg, "username"), get(c.cfg, "password"); u != "" && p != "" {
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p))
	}
	text := msg.Text
	if text == "" {
		text = msg.Title
	}
	body, status, err := c.base.do(ctx, "POST", serverURL+"/"+url.PathEscape(topic), headers, []byte(text))
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	var r struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &r)
	if r.Error != "" {
		return fmt.Errorf("发送失败: %s", r.Error)
	}
	return nil
}

// ---- 自定义 Webhook ----

type webhook struct {
	base *baseClient
	cfg  map[string]string
}

func (c *webhook) Send(ctx context.Context, msg Message) error {
	rawURL := get(c.cfg, "url")
	if rawURL == "" {
		return fmt.Errorf("缺少请求 URL")
	}
	method := strings.ToUpper(get(c.cfg, "method"))
	if method == "" {
		method = "POST"
	}
	endpoint := renderTemplate(rawURL, msg)

	headers := map[string]string{}
	if ct := get(c.cfg, "content_type"); ct != "" {
		headers["Content-Type"] = ct
	}

	var payload []byte
	if method != "GET" {
		if tmpl := c.cfg["body"]; strings.TrimSpace(tmpl) != "" {
			payload = []byte(renderTemplate(tmpl, msg))
		} else {
			// 默认发送一个通用 JSON
			payload, _ = json.Marshal(map[string]string{"title": msg.Title, "text": msg.Text})
			if headers["Content-Type"] == "" {
				headers["Content-Type"] = "application/json"
			}
		}
	}

	body, status, err := c.base.do(ctx, method, endpoint, headers, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	return nil
}
