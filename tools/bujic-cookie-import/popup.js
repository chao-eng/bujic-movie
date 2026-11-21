const $ = (id) => document.getElementById(id)

const STORAGE_KEYS = ['server', 'token']

function setStatus(text, cls = '') {
  const el = $('status')
  el.textContent = text
  el.className = cls
}

function show(id, visible) {
  $(id).hidden = !visible
}

async function readSession() {
  const { server = '', token = '' } = await chrome.storage.local.get(STORAGE_KEYS)
  return { server, token }
}

async function saveSession(server, token) {
  await chrome.storage.local.set({ server, token })
}

async function clearSession() {
  await chrome.storage.local.remove(STORAGE_KEYS)
}

/**
 * 读取当前标签页的 origin 与 localStorage 里的 JWT。
 *
 * 扩展自己看不到页面的 localStorage，必须用 scripting 注入到页面里去读。
 * chrome:// 页面、PDF 阅读器、或者没授权的页面会直接抛错，这时返回 null，
 * 由调用方提示用户先打开 Bujic Movie。
 */
async function readPageContext() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true })
  if (!tab?.id || !/^https?:/.test(tab.url || '')) return null
  try {
    const [injected] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => ({
        origin: location.origin,
        token: localStorage.getItem('token') || '',
      }),
    })
    const result = injected?.result
    if (!result?.token) return { origin: result?.origin || '', token: '' }
    return result
  } catch {
    return null
  }
}

/**
 * 用「页面 origin + 该页面的 token」请求一个受保护接口。
 *
 * 这是判断「当前标签页确实是 Bujic Movie 且登录有效」最可靠的办法：
 * 别的网站碰巧也有名为 token 的 localStorage 项时，这里的校验一定失败。
 */
async function validateSession(server, token) {
  try {
    const resp = await fetch(`${server}/api/v1/search/status`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (resp.status === 401) return { ok: false, reason: '登录已失效' }
    if (!resp.ok) return { ok: false, reason: `服务返回 ${resp.status}` }
    const payload = await resp.json().catch(() => ({}))
    if (payload.code !== 0) return { ok: false, reason: payload.msg || '校验失败' }
    return { ok: true, enabled: Boolean(payload.data?.enabled) }
  } catch {
    return { ok: false, reason: '无法连接该服务地址' }
  }
}

function renderLoggedOut(message) {
  show('session', false)
  show('login-btn', true)
  show('import-btn', false)
  show('logout-btn', false)
  setStatus(message || '', '')
}

function renderLoggedIn(server, enabled) {
  show('session', true)
  show('login-btn', false)
  show('import-btn', true)
  show('logout-btn', true)
  $('session-server').textContent = server
  $('session-scope').textContent = enabled ? '搜索下载已启用' : '搜索下载未启用'
  setStatus('', '')
}

function cookiesToString(cookies) {
  return cookies.map((c) => `${c.name}=${c.value}`).join('; ')
}

async function currentTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true })
  if (!tab?.url || !/^https?:/.test(tab.url)) {
    throw new Error('请先打开要导入 Cookie 的目标站点页面')
  }
  return tab
}

/**
 * 判断这个 Cookie 是否真的会被浏览器发送给该主机。
 *
 * 全量枚举会带出各种域名的 Cookie，不能按「后缀像」就收：
 * · domain 等于主机（host-only）——会发送；
 * · domain 是主机的父域（.example.com 对 a.example.com）——会发送；
 * · domain 是主机的兄弟/子域（api.example.com 对 example.com）——只发给那个子域，
 *   收进来就是把别人的会话串味，必须排除。
 */
function cookieAppliesTo(cookie, host) {
  const domain = (cookie.domain || '').toLowerCase().replace(/^\./, '')
  if (!domain) return false
  if (domain === host) return true
  // 父域 Cookie（domain 属性以 . 开头表示允许子域共享）。
  return cookie.domain.startsWith('.') && host.endsWith(`.${domain}`)
}

/**
 * 读取页面上脚本可见的 Cookie（document.cookie）。
 *
 * chrome.cookies 看不到的东西有两类：HttpOnly 的看不到（这是对的），
 * 以及浏览器对扩展隐藏的分区 Cookie（CHIPS）。document.cookie 恰好相反，
 * 所以两个来源都取一次再合并，覆盖面最大。
 */
async function readPageCookies(tab, hostname) {
  try {
    const [injected] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => document.cookie,
    })
    const raw = injected?.result || ''
    if (!raw) return []
    return raw
      .split(';')
      .map((part) => part.trim())
      .filter((part) => part.includes('='))
      .map((part) => {
        const idx = part.indexOf('=')
        return {
          name: part.slice(0, idx).trim(),
          value: part.slice(idx + 1),
          // 主机名由调用方传入：本函数在扩展上下文里跑，location 指向扩展自身。
          domain: hostname,
          fromPage: true,
        }
      })
      .filter((c) => c.name)
  } catch {
    return null
  }
}

/**
 * 收集当前站点可能用到的全部 Cookie，并留下探测记录用于排查。
 *
 * 只查 getAll({url}) 覆盖不全，这类站点常见三种漏法：
 * 1. Cookie 带 Path=/ 但写在父域或 www./api. 子域上；
 * 2. 用户通过的验证刚结束，还有中转 URL 残留的 Cookie；
 * 3. 浏览器把 Cookie 标成分区 Cookie（CHIPS），不对扩展暴露。
 *
 * 所以按「精确 URL → 根路径 → 各级父域」逐层放宽，再叠加一次 document.cookie。
 */
async function collectCookies(tab) {
  const tabUrl = tab.url
  const url = new URL(tabUrl)
  const queries = [
    { url: tabUrl },
    { url: `${url.origin}/` },
    { domain: url.hostname },
    { domain: `.${url.hostname}` },
  ]
  // 逐级父域：a.b.example.com -> b.example.com -> example.com
  const labels = url.hostname.split('.')
  for (let i = 1; i < labels.length - 1; i++) {
    queries.push({ domain: `.${labels.slice(i).join('.')}` })
  }

  const seen = new Map()
  const probe = []

  for (const query of queries) {
    const label = query.url ? `url=${query.url}` : `domain=${query.domain}`
    try {
      const found = await chrome.cookies.getAll(query)
      probe.push(`${label} → ${found.length} 个`)
      for (const cookie of found) {
        if (!seen.has(cookie.name)) seen.set(cookie.name, cookie)
      }
    } catch (err) {
      probe.push(`${label} → 查询被拒绝（${err?.message || err}）`)
    }
  }

  // 分区 Cookie（CHIPS）：Chrome 120+ 支持带 partitionKey 查询，这是官方正路。
  // 不带 partitionKey 的普通查询读不到它们，这也是上面全是 0 的原因之一。
  await collectPartitioned(url, seen, probe)

  // 兜底：全量枚举后只保留「浏览器确实会发给本站」的 Cookie。
  // 能捞到精确查询漏掉的（例如 Path 很深、或带 domain 的父域 Cookie）。
  try {
    const all = await chrome.cookies.getAll({})
    const host = url.hostname.toLowerCase()
    const matched = all.filter((cookie) => cookieAppliesTo(cookie, host))
    probe.push(`全量枚举 → 命中 ${matched.length} 个 / 共 ${all.length} 个`)
    for (const cookie of matched) {
      if (!seen.has(cookie.name)) seen.set(cookie.name, cookie)
    }
  } catch (err) {
    probe.push(`全量枚举 → 查询被拒绝（${err?.message || err}）`)
  }

  const pageCookies = await readPageCookies(tab, url.hostname)
  if (pageCookies === null) {
    probe.push('document.cookie → 无法注入页面读取')
  } else {
    probe.push(`document.cookie → ${pageCookies.length} 个`)
    for (const cookie of pageCookies) {
      if (!seen.has(cookie.name)) seen.set(cookie.name, cookie)
    }
  }

  // 最后一条路：从真实请求头里取。HttpOnly + 分区（CHIPS）Cookie 能随请求发出，
  // 但 chrome.cookies 按设计不暴露分区 Cookie，document.cookie 又被 HttpOnly 挡住，
  // 两条路都是 0。只有抓到实际发出去的 Cookie 头才拿得到。
  const header = await readCookieHeader(tab)
  if (header.cookies === null) {
    // 三种情况分开讲，修法完全不同，别让用户猜。
    if (header.sawCookieHeader) {
      probe.push('请求头 Cookie → 收到了请求但没解析出 Cookie 头')
    } else if (header.observed > 0) {
      probe.push(`请求头 Cookie → 收到 ${header.observed} 个请求但都没带 Cookie`)
    } else {
      probe.push('请求头 Cookie → 未能捕获（页面没有发出请求，或无法注入脚本）')
    }
  } else {
    probe.push(`请求头 Cookie → ${header.cookies.length} 个`)
    for (const cookie of header.cookies) {
      if (!seen.has(cookie.name)) seen.set(cookie.name, cookie)
    }
  }

  return { cookies: [...seen.values()], probe }
}

/**
 * 读取分区（CHIPS）Cookie。
 *
 * 分区 Cookie 按「顶层站点」隔离，普通 getAll 查不到。Chrome 120 起支持在查询里
 * 传 partitionKey，用来取出以当前站点为分区键的那些 Cookie。
 * 老版本 Chrome 没有这个能力，会走到 catch，此时靠请求头兜底。
 */
async function collectPartitioned(url, seen, probe) {
  try {
    const found = await chrome.cookies.getAll({
      domain: url.hostname,
      partitionKey: { topLevelSite: url.origin },
    })
    probe.push(`分区 Cookie（${url.origin}）→ ${found.length} 个`)
    for (const cookie of found) {
      if (!seen.has(cookie.name)) seen.set(cookie.name, cookie)
    }
  } catch (err) {
    probe.push(`分区 Cookie → 当前浏览器不支持该查询（${err?.message || err}）`)
  }
}

/**
 * 从该标签页真实发出的请求里抓 Cookie 请求头。
 *
 * 做法：先挂一个 webRequest 监听，再让页面自己发一个同源请求（fetch 一个静态资源），
 * 从而拿到浏览器实际附加的 Cookie 头。这能覆盖 chrome.cookies 读不到的分区 Cookie。
 *
 * 返回 null 表示没捕获到（例如页面已经被关掉，或该请求没有 Cookie）。
 */
function readCookieHeader(tab) {
  return new Promise((resolve) => {
    const origin = new URL(tab.url).origin
    let settled = false
    // 记录观察到的请求数：区分「请求根本没发出」和「发了但没带 Cookie」。
    let observed = 0
    let sawCookieHeader = false

    const finish = (value) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      try {
        chrome.webRequest.onBeforeSendHeaders.removeListener(onHeaders)
      } catch {
        // 监听器可能已随页面卸载，忽略。
      }
      resolve({ cookies: value, observed, sawCookieHeader })
    }

    const onHeaders = (details) => {
      if (details.tabId !== tab.id) return
      if (!details.url.startsWith(origin)) return
      observed++
      const header = (details.requestHeaders || []).find(
        (h) => h.name.toLowerCase() === 'cookie',
      )
      if (!header?.value) return
      sawCookieHeader = true
      finish(parseCookieHeader(header.value, new URL(details.url).hostname))
    }

    // 5 秒内没抓到就放弃，避免弹层一直转圈。
    const timer = setTimeout(() => finish(null), 5000)

    try {
      chrome.webRequest.onBeforeSendHeaders.addListener(
        onHeaders,
        { urls: [`${origin}/*`], types: ['xmlhttprequest', 'main_frame', 'image', 'other'] },
        // MV3 里 Cookie 属于敏感请求头，只写 requestHeaders 是看不到它的，
        // 必须显式加 extraHeaders。
        ['requestHeaders', 'extraHeaders'],
      )
    } catch {
      finish(null)
      return
    }

    // 触发一次真实的同源请求。加随机参数避免命中缓存（缓存响应不带请求头）。
    // 同时用 content 脚本读一次 location，确认页面本身可注入。
    chrome.scripting
      .executeScript({
        target: { tabId: tab.id },
        func: async (u) => {
          try {
            await fetch(u, { cache: 'no-store', credentials: 'same-origin' })
            return 'ok'
          } catch {
            return 'failed'
          }
        },
        // 用当前页面自己的地址：它对站点一定存在、一定同源，也不会像
        // favicon 那样被抓成 404 或被 service worker 直接短路。
        args: [`${tab.url.split('#')[0]}${tab.url.includes('?') ? '&' : '?'}_cb=${Date.now()}`],
      })
      .catch(() => finish(null))
  })
}

/** 把 "a=1; b=2" 解析成与 chrome.cookies 一致的结构。 */
function parseCookieHeader(raw, host) {
  return String(raw || '')
    .split(';')
    .map((part) => part.trim())
    .filter((part) => part.includes('='))
    .map((part) => {
      const idx = part.indexOf('=')
      return { name: part.slice(0, idx).trim(), value: part.slice(idx + 1), domain: host }
    })
    .filter((c) => c.name)
}

/**
 * 读取当前页面的可见状态，用来区分「真的没有 Cookie」和「还在验证页上」。
 *
 * Cloudflare 挑战页的标题是 "Just a moment..."，URL 也可能带 __cf_chl_ 参数。
 * 这时站点还没发任何业务 Cookie，抓取必然是空的 —— 提示用户等页面跳转，
 * 比抛一句「没有可导入的 Cookie」有用得多。
 */
async function inspectPage(tab) {
  try {
    const [injected] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => ({
        title: document.title,
        url: location.href,
        challenge:
          /just a moment|请稍候|attention required|checking your browser/i.test(document.title) ||
          location.href.includes('__cf_chl') ||
          location.href.includes('/cdn-cgi/'),
      }),
    })
    return injected?.result ?? null
  } catch {
    // 权限不足或页面不可注入时退化为「不知道」，不阻断流程。
    return null
  }
}

async function importSession(indexerId, manualCookies = '') {
  const { server, token } = await readSession()
  if (!server || !token) throw new Error('尚未连接 Bujic Movie，请先点「使用当前页面的登录状态」')

  const tab = await currentTab()
  const url = new URL(tab.url)

  const page = await inspectPage(tab)
  if (page?.challenge) {
    throw new Error(
      `「${url.hostname}」还停在 Cloudflare 验证页（${page.title || 'Just a moment…'}）。\n` +
        '请等页面真正进入站点、能看到内容后再抓取——验证页本身不会下发 Cookie。',
    )
  }

  // 手动粘贴优先：用户明确给了内容就不再去猜浏览器为什么不暴露。
  if (manualCookies.trim()) {
    const draft = manualCookies.trim()
    const body = {
      host: url.hostname,
      url: tab.url,
      cookies: draft,
      user_agent: navigator.userAgent,
    }
    if (indexerId) body.indexer_id = indexerId
    const resp = await fetch(`${server}/api/v1/search/cookie-import`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify(body),
    })
    const payload = await resp.json().catch(() => ({}))
    return { resp, payload }
  }

  const { cookies, probe } = await collectCookies(tab)
  if (!cookies.length) {
    // 把每次查询的真实结果摊开：这能直接区分「Cookie 在别的域上」、
    // 「浏览器不向扩展暴露」和「站点确实没写 Cookie」。
    const where = page?.title ? `页面标题「${page.title}」` : '当前标签页'
    throw new Error(
      `当前站点（${url.hostname}）没有可读取的 Cookie。${where}\n\n` +
        '探测结果：\n' +
        probe.map((line) => `· ${line}`).join('\n') +
        '\n\n· 「请求头 Cookie」也是 0，说明浏览器确实没给这个站点发 Cookie；\n' +
        '  先在页面上正常操作一次（点个链接或刷新），再回来抓取。\n' +
        '· 若请求头一栏显示「未能捕获」，通常是页面刚打开还没发出请求；\n' +
        '  刷新页面后重试即可。',
    )
  }

  const body = {
    host: url.hostname,
    url: tab.url,
    cookies: cookiesToString(cookies),
    user_agent: navigator.userAgent,
  }
  if (indexerId) body.indexer_id = indexerId

  const resp = await fetch(`${server}/api/v1/search/cookie-import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(body),
  })
  const payload = await resp.json().catch(() => ({}))
  return { resp, payload }
}

function renderCandidates(candidates) {
  const list = $('candidates')
  list.innerHTML = ''
  setStatus('多个站点匹配该域名，请选择：', 'err')
  for (const item of candidates) {
    const li = document.createElement('li')
    const btn = document.createElement('button')
    btn.textContent = `${item.name}（${item.host}）`
    btn.addEventListener('click', async () => {
      try {
        setStatus('导入中…')
        const { resp, payload } = await importSession(item.id)
        if (!resp.ok) throw new Error(payload.msg || `请求失败 ${resp.status}`)
        list.innerHTML = ''
        setStatus(payload.data?.message || '导入成功', 'ok')
      } catch (err) {
        setStatus(String(err.message || err), 'err')
      }
    })
    li.appendChild(btn)
    list.appendChild(li)
  }
}

/** 打开插件时：优先复用已保存的会话，其次尝试从当前页面自动接管。 */
async function boot() {
  renderLoggedOut('')
  $('login-btn').disabled = true
  setStatus('正在检查登录状态…')

  const page = await readPageContext()
  const pageToken = page?.token || ''
  let { server, token } = await readSession()

  // 当前标签页就是已登录的 Bujic Movie：它的 origin 与 token 最权威，
  // 直接接管并覆盖旧值（服务地址换端口时也能自愈）。
  if (pageToken && page.origin) {
    server = page.origin
    token = pageToken
    await saveSession(server, token)
  }

  if (!server || !token) {
    $('login-btn').disabled = false
    renderLoggedOut(
      pageToken
        ? ''
        : '还没连接过。请在浏览器里打开并登录 Bujic Movie，再点下面的按钮。',
    )
    return
  }

  const result = await validateSession(server, token)
  $('login-btn').disabled = false
  if (result.ok) {
    renderLoggedIn(server, result.enabled)
    return
  }
  await clearSession()
  renderLoggedOut(
    page ? `登录已失效（${result.reason}）。若已在网页重新登录，再点一次按钮。` : `${result.reason}，请打开 Bujic Movie 后重新登录。`,
  )
}

$('login-btn').addEventListener('click', async () => {
  const btn = $('login-btn')
  btn.disabled = true
  try {
    const page = await readPageContext()
    if (!page) {
      setStatus('当前标签页无法读取，请切到 Bujic Movie 页面再试。', 'err')
      return
    }
    if (!page.token) {
      setStatus('当前标签页不是 Bujic Movie 的已登录页面，请先打开并登录。', 'err')
      return
    }

    setStatus('正在校验登录状态…')
    const result = await validateSession(page.origin, page.token)
    if (!result.ok) {
      setStatus(`这个页面不是 Bujic Movie，或登录已失效（${result.reason}）。`, 'err')
      return
    }
    await saveSession(page.origin, page.token)
    renderLoggedIn(page.origin, result.enabled)
    setStatus('已复用当前页面的登录状态', 'ok')
  } catch (err) {
    setStatus(String(err.message || err), 'err')
  } finally {
    btn.disabled = false
  }
})

$('import-btn').addEventListener('click', async () => {
  const btn = $('import-btn')
  btn.disabled = true
  try {
    setStatus('读取当前站点 Cookie…')
    const { resp, payload } = await importSession()
    if (resp.status === 409 && payload.data?.candidates?.length) {
      renderCandidates(payload.data.candidates)
      return
    }
    if (!resp.ok) throw new Error(payload.msg || `请求失败 ${resp.status}`)
    setStatus(payload.data?.message || '导入成功', 'ok')
  } catch (err) {
    setStatus(String(err.message || err), 'err')
  } finally {
    btn.disabled = false
  }
})

$('manual-import-btn').addEventListener('click', async () => {
  const btn = $('manual-import-btn')
  btn.disabled = true
  try {
    const text = $('manual-cookies').value
    if (!text.trim()) throw new Error('请先粘贴 Cookie 内容')
    setStatus('导入中…')
    const { resp, payload } = await importSession(0, text)
    if (resp.status === 409 && payload.data?.candidates?.length) {
      renderCandidates(payload.data.candidates)
      return
    }
    if (!resp.ok) throw new Error(payload.msg || `请求失败 ${resp.status}`)
    setStatus(payload.data?.message || '导入成功', 'ok')
    $('manual-cookies').value = ''
  } catch (err) {
    setStatus(String(err.message || err), 'err')
  } finally {
    btn.disabled = false
  }
})

$('logout-btn').addEventListener('click', async () => {
  await clearSession()
  $('candidates').innerHTML = ''
  renderLoggedOut('已断开连接')
})

boot()
