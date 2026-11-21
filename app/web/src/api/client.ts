import axios from 'axios'

const client = axios.create({
  baseURL: '',
  timeout: 15000,
})

// Request interceptor to attach JWT token
client.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

/**
 * 只有「登录态真的失效」才该把用户踢回登录页。
 *
 * 业务接口里 401 也可能表示「你配置的某个外部服务认证失败」
 * （例如站点或下载器的密码错误，错误码 401xx）。过去这种响应会清掉本地
 * token 并跳转登录页，用户点一次「测试」就被登出。现在改为按错误码判定：
 * 会话相关的 40101 才登出，目标服务认证失败原样抛给调用方提示。
 */
const SESSION_INVALID_CODES = new Set([40101])

function isSessionInvalid(error: any): boolean {
  const status = error?.response?.status
  if (status !== 401) return false
  const code = error?.response?.data?.code
  // 没有业务错误码时按登录失效处理（例如反向代理直接返回 401）。
  if (code === undefined || code === null) return true
  return SESSION_INVALID_CODES.has(Number(code))
}

client.interceptors.response.use(
  (response) => {
    return response.data
  },
  (error) => {
    if (isSessionInvalid(error)) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error.response ? error.response.data : error)
  }
)

export default client
