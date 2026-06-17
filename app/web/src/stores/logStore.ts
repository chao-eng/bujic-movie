import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useLogStore = defineStore('logStore', () => {
  const logs = ref<any[]>([])
  const isConnected = ref(false)
  let socket: WebSocket | null = null
  let reconnectTimeout: any = null

  const initWebSocket = () => {
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
      return
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/v1/ws`
    
    console.log('[LogStore] Initializing WebSocket connection to', wsUrl)
    socket = new WebSocket(wsUrl)

    socket.onopen = () => {
      isConnected.value = true
      if (logs.value.length > 0) {
        logs.value.push({
          timestamp: new Date().toLocaleTimeString(),
          level: 'INFO',
          message: 'WebSocket 实时日志通道重新连接成功。',
        })
      } else {
        logs.value.push({
          timestamp: new Date().toLocaleTimeString(),
          level: 'INFO',
          message: 'WebSocket 实时日志通道连接成功。',
        })
      }
    }

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'log') {
          logs.value.push(msg.payload)
          if (logs.value.length > 500) {
            logs.value.shift()
          }
        }
      } catch (e) {
        console.error('[LogStore] WebSocket parse error', e)
      }
    }

    socket.onerror = () => {
      isConnected.value = false
      logs.value.push({
        timestamp: new Date().toLocaleTimeString(),
        level: 'ERROR',
        message: 'WebSocket 连接出现错误。',
      })
    }

    socket.onclose = () => {
      isConnected.value = false
      logs.value.push({
        timestamp: new Date().toLocaleTimeString(),
        level: 'WARN',
        message: 'WebSocket 日志通道连接断开，正在尝试重连...',
      })
      if (reconnectTimeout) clearTimeout(reconnectTimeout)
      reconnectTimeout = setTimeout(initWebSocket, 5000)
    }
  }

  const disconnect = () => {
    console.log('[LogStore] Disconnecting WebSocket')
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
      reconnectTimeout = null
    }
    if (socket) {
      socket.close()
      socket = null
    }
    isConnected.value = false
  }

  return {
    logs,
    isConnected,
    initWebSocket,
    disconnect
  }
})
