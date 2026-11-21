import { describe, expect, it } from 'vitest'

/**
 * 回归：会话失效才登出。
 *
 * 过去 axios 拦截器把任何 401 都当作登录过期，于是「下载器密码错误」这类
 * 目标服务认证失败会把用户踢回登录页。这里固化判定规则：
 * 只有 40101（会话失效）或没有业务错误码的 401 才清 token。
 */

const SESSION_INVALID_CODES = new Set([40101])

function isSessionInvalid(error: any): boolean {
  const status = error?.response?.status
  if (status !== 401) return false
  const code = error?.response?.data?.code
  if (code === undefined || code === null) return true
  return SESSION_INVALID_CODES.has(Number(code))
}

const unauthorized = (code?: number) => ({
  response: { status: 401, data: code === undefined ? {} : { code } },
})

describe('401 handling', () => {
  it('logs out when the session itself is invalid', () => {
    expect(isSessionInvalid(unauthorized(40101))).toBe(true)
  })

  it('logs out on a bare 401 without a business code', () => {
    expect(isSessionInvalid(unauthorized())).toBe(true)
  })

  it('does not log out when a target service rejects credentials', () => {
    // 站点或下载器认证失败：用户仍处于登录状态。
    expect(isSessionInvalid(unauthorized(40102))).toBe(false)
  })

  it('ignores non-401 statuses', () => {
    expect(isSessionInvalid({ response: { status: 422, data: { code: 40102 } } })).toBe(false)
    expect(isSessionInvalid({ response: { status: 500 } })).toBe(false)
    expect(isSessionInvalid(new Error('network'))).toBe(false)
  })
})
