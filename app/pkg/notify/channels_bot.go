package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// markdownBody builds a markdown message body (title + quoted text + optional image).
func markdownBody(msg Message) string {
	var b strings.Builder
	if msg.Title != "" {
		b.WriteString("# " + msg.Title + "\n\n")
	}
	if msg.Text != "" {
		b.WriteString("> " + msg.Text + "\n\n")
	}
	if msg.Image != "" {
		b.WriteString("![](" + msg.Image + ")")
	}
	s := b.String()
	if s == "" {
		s = msg.Title
	}
	return s
}

// plainBody builds a plain-text message body.
func plainBody(msg Message) string {
	parts := []string{}
	if msg.Title != "" {
		parts = append(parts, msg.Title)
	}
	if msg.Text != "" {
		parts = append(parts, msg.Text)
	}
	if msg.Link != "" {
		parts = append(parts, msg.Link)
	}
	return strings.Join(parts, "\n")
}

// ---- 企业微信机器人 ----

type wecomBot struct {
	base *baseClient
	cfg  map[string]string
}

func (c *wecomBot) Send(ctx context.Context, msg Message) error {
	key := get(c.cfg, "key")
	if key == "" {
		return fmt.Errorf("缺少机器人 Key")
	}
	payload, _ := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": markdownBody(msg)},
	})
	body, status, err := c.base.postJSON(ctx, "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key="+url.QueryEscape(key), payload)
	if err != nil {
		return err
	}
	return checkErrcode(body, status)
}

// ---- 钉钉机器人 ----

type dingtalkBot struct {
	base *baseClient
	cfg  map[string]string
}

func (c *dingtalkBot) Send(ctx context.Context, msg Message) error {
	token := get(c.cfg, "access_token")
	if token == "" {
		return fmt.Errorf("缺少 Access Token")
	}
	q := url.Values{}
	q.Set("access_token", token)
	if secret := get(c.cfg, "secret"); secret != "" {
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts + "\n" + secret))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		q.Set("timestamp", ts)
		q.Set("sign", sign)
	}
	payload, _ := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"title": titleOr(msg), "text": markdownBody(msg)},
	})
	body, status, err := c.base.postJSON(ctx, "https://oapi.dingtalk.com/robot/send?"+q.Encode(), payload)
	if err != nil {
		return err
	}
	return checkErrcode(body, status)
}

// ---- 飞书机器人 ----

type feishuBot struct {
	base *baseClient
	cfg  map[string]string
}

func (c *feishuBot) Send(ctx context.Context, msg Message) error {
	token := get(c.cfg, "access_token")
	if token == "" {
		return fmt.Errorf("缺少 Webhook Token")
	}
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": plainBody(msg)},
	}
	if secret := get(c.cfg, "secret"); secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		// 飞书签名：以 "{timestamp}\n{secret}" 作为 HMAC 密钥，对空串签名
		mac := hmac.New(sha256.New, []byte(ts+"\n"+secret))
		payload["timestamp"] = ts
		payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	data, _ := json.Marshal(payload)
	body, status, err := c.base.postJSON(ctx, "https://open.feishu.cn/open-apis/bot/v2/hook/"+token, data)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	var r struct {
		Code       *int   `json:"code"`
		StatusCode *int   `json:"StatusCode"`
		Msg        string `json:"msg"`
	}
	_ = json.Unmarshal(body, &r)
	if (r.Code != nil && *r.Code != 0) || (r.StatusCode != nil && *r.StatusCode != 0) {
		return fmt.Errorf("飞书返回失败: %s", string(body))
	}
	return nil
}

func titleOr(msg Message) string {
	if msg.Title != "" {
		return msg.Title
	}
	return "通知"
}

// checkErrcode handles the 企业微信/钉钉 style response: {"errcode":0,"errmsg":"ok"}.
func checkErrcode(body []byte, status int) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	var r struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if r.Errcode != 0 {
		return fmt.Errorf("发送失败: errcode=%d, %s", r.Errcode, r.Errmsg)
	}
	return nil
}
