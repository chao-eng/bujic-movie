# Bujic Movie Cookie 导入（Chrome / Edge 扩展）

把「已经通过 Cloudflare 人机验证」的浏览器会话导入 Bujic Movie，让服务端复用同一个 `cf_clearance` 与 User-Agent。

## 安装

1. 打开 `chrome://extensions`（Edge 为 `edge://extensions`）。
2. 打开「开发者模式」。
3. 点「加载已解压的扩展程序」，选择本目录。

## 使用

不再需要手填服务地址和令牌。

1. 浏览器里先登录 Bujic Movie。
2. 打开被 Cloudflare 拦截的站点，手动完成验证，确认能正常看到页面。
3. 点扩展图标，再点「抓取当前站点并导入」。扩展会读取该站点的全部 Cookie 与当前 User-Agent，提交到 `POST /api/v1/search/cookie-import`。
4. 回到 Bujic Movie 的「站点」页点一次「测试」，应变为可用。

## 登录状态怎么来的

扩展不要求你粘贴 JWT，而是自己判断当前标签页是不是 Bujic Movie 并顺手取走登录态：

1. **读取当前标签页**：用 `chrome.scripting` 注入脚本，读取 `location.origin` 和 `localStorage.token`。
   扩展 API 本身看不到网页的 `localStorage`，所以必须注入到页面里去读。
2. **校验这个页面确实是 Bujic Movie**：拿页面 origin + 该页面里的 token 去请求 `/api/v1/search/status`。
   返回 200 才算通过，401 一律视为「不是这个站点，或登录已失效」。这样即使别的网站碰巧也有名叫 `token` 的 localStorage 项也不会误判。
3. **记住它**：校验通过后把 origin + token 存进 `chrome.storage.local`。
4. **下次直接用**：再次打开插件时先用存下的值请求同一个接口校验，可用就直接复用，不再要求点按钮。

如果服务换了地址或端口，下次在浏览器里登录新地址后点一次按钮即可自动覆盖旧值。

## 界面状态

- **未连接**：只有一个「使用当前页面的登录状态」按钮。点击时自动判定当前标签页；不是 Bujic Movie 就提示先打开并登录。
- **已连接**：显示服务地址与「搜索下载已启用/未启用」，主按钮变成「抓取当前站点并导入」，另有「断开连接」。
- **登录失效**（令牌过期或被清理）：自动清除本地记录并回到未连接状态，提示重新登录。

## 为什么有些站点读不到 Cookie

`chrome.cookies` 和 `document.cookie` 都读不到、请求却带着 Cookie 的站点，通常是
**HttpOnly + 分区（CHIPS）Cookie**：浏览器按设计不把分区 Cookie 暴露给扩展 API，
HttpOnly 又让它对页面脚本不可见（1337x 的 `cf_clearance` 就是这种）。

对此扩展有两条补充途径：

1. **分区查询**（Chrome 120+）：`chrome.cookies.getAll` 支持传 `partitionKey`，
   可以直接把以当前站点为分区键的 Cookie 取出来。这是官方正路，优先使用。
2. **真实请求头**：先挂 `webRequest` 监听，再让页面自己发一个同源请求，读出浏览器
   实际附加的 `Cookie` 头。老版本 Chrome 或分区查询仍取不到时靠它兜底。
   读取 `Cookie` 头必须带 `extraHeaders` 选项——MV3 里它是敏感头，不显式声明就拿不到。

## 抓不到 Cookie 时

扩展会依次尝试六种方式：精确 URL、根路径、各级父域、全量枚举后按域名过滤、
分区 Cookie 查询、页面内 `document.cookie`，最后再抓一次真实请求头。仍然为空时，界面会列出每一步的
探测结果（例如「全量枚举 → 命中 0 个 / 共 2977 个」「请求头 Cookie → 1 个」），
据此判断是哪种情况。

如果以上都拿不到，用「抓不到 Cookie？手动粘贴」兜底。注意 `document.cookie` 拿不到
HttpOnly 的 Cookie（`cf_clearance` 多半属于这类），所以要在 F12 的
**应用 → 存储 → Cookie** 面板里，或网络中某个请求的 Cookie 请求头里，把
`name=value` 复制出来粘贴。

按顺序排查：

1. **还在验证页上**。Cloudflare 的验证页标题是 “Just a moment…”，它本身不下发任何 Cookie。
   等页面真正进入站点、能看到内容后再抓。扩展现在会直接识别这种情况并提示。
2. **Cookie 在别的子域上**。有些站点把会话写在 `www.` 或 `api.` 子域。
   扩展会按「精确 URL → 根路径 → 各级父域」逐层收集，但跨子域仍需在那个子域页面上抓。
3. **扩展刚加载/更新过**。改过 `manifest.json` 的权限（例如新增 `webRequest`）后
   必须在 `chrome://extensions` 点一次刷新，否则相关调用会因权限不足静默失败。
4. **页面刚打开、还没发过请求**。请求头抓取依赖页面上真实发出的请求；刷新一下页面
   再抓即可。

## 关键限制

- `cf_clearance` 绑定 **出口 IP + User-Agent**。如果 Bujic Movie 走代理而浏览器直连，Cookie 不会生效。请让两边使用同一出口 IP（例如浏览器也配置同一个代理），并在站点配置里填同一个代理。
- Cookie 会过期（通常几小时到几天），失效后重新执行一次即可。
- 扩展只保存服务地址和 Token 到本地 `chrome.storage`；Cookie 只在你点按钮的那一刻发给
  你自己的服务地址，不做其它用途。`webRequest` 权限仅用于读取目标站点的 Cookie 请求头。
- 需要 Chrome 88+ / Edge 88+（`chrome.scripting`）。若浏览器策略禁用了扩展注入页面脚本，按钮会提示「当前标签页无法读取」。
