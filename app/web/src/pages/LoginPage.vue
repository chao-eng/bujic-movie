<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Film, User, Lock, Loader2 } from 'lucide-vue-next'
import { encryptAESGCM } from '@/lib/crypto'

const router = useRouter()
const username = ref('admin')
const password = ref('')
const errorMsg = ref('')
const isLoading = ref(false)

const handleLogin = async () => {
  if (!password.value) {
    errorMsg.value = '请输入密码'
    return
  }

  isLoading.value = true
  errorMsg.value = ''

  try {
    // 1. Fetch ephemeral encryption challenge key
    const keyRes = await axios.get('/api/v1/auth/login-key')
    if (!keyRes.data || keyRes.data.code !== 0) {
      throw new Error(keyRes.data?.msg || '无法获取加密密钥')
    }
    const { key_id, key } = keyRes.data.data

    // 2. Encrypt the password using GCM
    const encrypted = await encryptAESGCM(password.value, key)

    // 3. Submit encrypted credentials
    const res = await axios.post('/api/v1/auth/login', {
      username: username.value,
      encrypted_password: encrypted.ciphertext,
      key_id,
      iv: encrypted.iv,
    })

    if (res.data && res.data.code === 0) {
      localStorage.setItem('token', res.data.data.token)
      router.push('/dashboard')
    } else {
      errorMsg.value = res.data.msg || '登录失败'
    }
  } catch (err: any) {
    errorMsg.value = err.response?.data?.msg || err.message || '网络错误，请稍后重试'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <div class="h-screen w-screen bg-slate-950 flex items-center justify-center font-sans">
    <!-- Ambient blurred background blobs -->
    <div class="absolute inset-0 overflow-hidden pointer-events-none">
      <div class="absolute top-1/4 left-1/4 w-96 h-96 bg-amber-500/10 rounded-full blur-3xl"></div>
      <div class="absolute bottom-1/4 right-1/4 w-96 h-96 bg-blue-500/10 rounded-full blur-3xl"></div>
    </div>

    <!-- Login Card -->
    <Card class="w-full max-w-md bg-slate-900/80 border-slate-800 backdrop-blur-xl relative z-10 text-slate-100">
      <CardHeader class="space-y-1 text-center">
        <div class="mx-auto bg-amber-500/10 p-3 rounded-full w-12 h-12 flex items-center justify-center mb-2">
          <Film class="h-6 w-6 text-amber-500" />
        </div>
        <CardTitle class="text-2xl font-extrabold text-slate-100 tracking-wider">BUJIC MOVIE</CardTitle>
        <CardDescription class="text-slate-400">登入您的媒体管家控制台</CardDescription>
      </CardHeader>
      <CardContent class="grid gap-4">
        <!-- Error Alert -->
        <div v-if="errorMsg" class="bg-rose-500/10 border border-rose-500/20 text-rose-400 text-xs px-3 py-2.5 rounded-lg">
          {{ errorMsg }}
        </div>

        <form @submit.prevent="handleLogin" class="space-y-4">
          <!-- Username -->
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">用户名</label>
            <div class="relative flex items-center">
              <User class="absolute left-3 h-4 w-4 text-slate-500" />
              <input
                v-model="username"
                type="text"
                class="w-full bg-slate-950 border border-slate-800 rounded-lg pl-10 pr-4 py-2.5 text-sm focus:border-amber-500 focus:outline-none text-slate-100 transition-colors"
                placeholder="请输入用户名"
                required
              />
            </div>
          </div>

          <!-- Password -->
          <div class="space-y-1.5">
            <label class="text-xs font-semibold text-slate-400">密码</label>
            <div class="relative flex items-center">
              <Lock class="absolute left-3 h-4 w-4 text-slate-500" />
              <input
                v-model="password"
                type="password"
                class="w-full bg-slate-950 border border-slate-800 rounded-lg pl-10 pr-4 py-2.5 text-sm focus:border-amber-500 focus:outline-none text-slate-100 transition-colors"
                placeholder="请输入密码"
                required
              />
            </div>
          </div>

          <!-- Submit -->
          <Button
            type="submit"
            :disabled="isLoading"
            class="w-full bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold py-2.5 rounded-lg flex items-center justify-center gap-2 transition-all mt-6"
          >
            <Loader2 v-if="isLoading" class="h-4 w-4 animate-spin" />
            <span>{{ isLoading ? '登录中...' : '立即登入' }}</span>
          </Button>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
