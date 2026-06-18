package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// descBody builds a text/markdown description (text + optional image).
func descBody(msg Message) string {
	d := msg.Text
	if d == "" {
		d = msg.Title
	}
	if msg.Image != "" {
		d += "\n\n![](" + msg.Image + ")"
	}
	return d
}

// checkCode handles responses shaped like {"code":N,"message|msg|error":"..."}.
func checkCode(body []byte, status, successCode int) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if r.Code != successCode {
		m := r.Message
		if m == "" {
			m = r.Msg
		}
		if m == "" {
			m = r.Error
		}
		return fmt.Errorf("发送失败: code=%d, %s", r.Code, m)
	}
	return nil
}

// ---- Server酱 ----

type serverChan struct {
	base *baseClient
	cfg  map[string]string
}

func (c *serverChan) Send(ctx context.Context, msg Message) error {
	key := get(c.cfg, "send_key")
	if key == "" {
		return fmt.Errorf("缺少 SendKey")
	}
	payload, _ := json.Marshal(map[string]any{
		"title": titleOr(msg),
		"desp":  descBody(msg),
	})
	body, status, err := c.base.postJSON(ctx, "https://sctapi.ftqq.com/"+url.PathEscape(key)+".send", payload)
	if err != nil {
		return err
	}
	return checkCode(body, status, 0)
}

// ---- Server酱 3 ----
//
// 官方 API 参考：https://sct.ftqq.com/sendkey
//   curl "https://<uid>.push.ft07.com/send/<sendkey>.send?title=<title>&desp=<desp>"
//   一行代码即可调用，中文参数需要 UrlEncode，长内容建议使用 POST。
//
// 我们用 application/x-www-form-urlencoded POST + url.Values 自动完成编码，
// 既满足"中文参数需要UrlEncode"也满足"长内容建议使用POST"。
type serverChan3 struct {
	base *baseClient
	cfg  map[string]string
}

func (c *serverChan3) Send(ctx context.Context, msg Message) error {
	uid := get(c.cfg, "uid")
	sendKey := get(c.cfg, "send_key")
	if uid == "" || sendKey == "" {
		return fmt.Errorf("缺少 UID 或 SendKey")
	}
	form := url.Values{}
	form.Set("title", titleOr(msg))
	form.Set("desp", descBody(msg))
	endpoint := fmt.Sprintf("https://%s.push.ft07.com/send/%s.send", uid, sendKey)
	body, status, err := c.base.do(ctx, "POST", endpoint,
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		[]byte(form.Encode()),
	)
	if err != nil {
		return err
	}
	return checkCode(body, status, 0)
}

// ---- Bark ----

type bark struct {
	base *baseClient
	cfg  map[string]string
}

func (c *bark) Send(ctx context.Context, msg Message) error {
	serverURL := get(c.cfg, "server_url")
	if serverURL == "" {
		serverURL = "https://api.day.app"
	}
	serverURL = strings.TrimRight(serverURL, "/")
	pushKey := get(c.cfg, "push_key")
	if pushKey == "" {
		return fmt.Errorf("缺少推送 Key")
	}
	payload := map[string]any{
		"title": titleOr(msg),
		"body":  bodyOr(msg),
	}
	if g := get(c.cfg, "group"); g != "" {
		payload["group"] = g
	}
	if s := get(c.cfg, "sound"); s != "" {
		payload["sound"] = s
	}
	if msg.Link != "" {
		payload["url"] = msg.Link
	}
	if msg.Image != "" {
		payload["icon"] = msg.Image
	}
	data, _ := json.Marshal(payload)
	body, status, err := c.base.postJSON(ctx, serverURL+"/"+url.PathEscape(pushKey), data)
	if err != nil {
		return err
	}
	return checkCode(body, status, 200)
}

func bodyOr(msg Message) string {
	if msg.Text != "" {
		return msg.Text
	}
	return msg.Title
}

// ---- PushPlus ----

type pushPlus struct {
	base *baseClient
	cfg  map[string]string
}

func (c *pushPlus) Send(ctx context.Context, msg Message) error {
	token := get(c.cfg, "token")
	if token == "" {
		return fmt.Errorf("缺少用户令牌")
	}
	payload := map[string]any{
		"token":    token,
		"title":    titleOr(msg),
		"content":  descBody(msg),
		"template": "markdown",
		"channel":  "wechat",
	}
	if topic := get(c.cfg, "topic"); topic != "" {
		payload["topic"] = topic
	}
	data, _ := json.Marshal(payload)
	body, status, err := c.base.postJSON(ctx, "http://www.pushplus.plus/send", data)
	if err != nil {
		return err
	}
	return checkCode(body, status, 200)
}

// ---- PushDeer ----

type pushDeer struct {
	base *baseClient
	cfg  map[string]string
}

func (c *pushDeer) Send(ctx context.Context, msg Message) error {
	serverURL := get(c.cfg, "server_url")
	if serverURL == "" {
		serverURL = "https://api2.pushdeer.com"
	}
	serverURL = strings.TrimRight(serverURL, "/")
	pushKey := get(c.cfg, "push_key")
	if pushKey == "" {
		return fmt.Errorf("缺少 PushKey")
	}
	payload, _ := json.Marshal(map[string]any{
		"pushkey": pushKey,
		"text":    titleOr(msg),
		"desp":    descBody(msg),
		"type":    "markdown",
	})
	body, status, err := c.base.postJSON(ctx, serverURL+"/message/push", payload)
	if err != nil {
		return err
	}
	return checkCode(body, status, 0)
}
