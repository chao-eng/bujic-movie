<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { LockIcon, UserIcon } from 'lucide-vue-next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
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
    // 1. 取一次性的加密公钥
    const keyRes = await axios.get('/api/v1/auth/login-key')
    if (!keyRes.data || keyRes.data.code !== 0) {
      throw new Error(keyRes.data?.msg || '无法获取加密密钥')
    }
    const { key_id, key } = keyRes.data.data

    // 2. 用 GCM 加密密码
    const encrypted = await encryptAESGCM(password.value, key)

    // 3. 提交密文
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
  <div class="bg-background flex h-screen w-screen items-center justify-center font-sans">
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div class="bg-primary/10 absolute top-1/4 left-1/4 size-96 rounded-full blur-3xl"></div>
      <div class="bg-info/10 absolute right-1/4 bottom-1/4 size-96 rounded-full blur-3xl"></div>
    </div>

    <Card class="relative z-10 w-full max-w-md backdrop-blur-xl">
      <CardHeader class="justify-items-center py-6 text-center">
        <img src="/favicon.png" alt="Bujic Movie" class="bg-muted ring-border mb-2 size-14 rounded-xl ring-1" />
        <CardTitle class="text-2xl font-extrabold tracking-wider">BUJIC MOVIE</CardTitle>
        <CardDescription>登入您的媒体管家控制台</CardDescription>
      </CardHeader>

      <CardContent class="pb-6">
        <form class="flex flex-col gap-4" @submit.prevent="handleLogin">
          <Alert v-if="errorMsg" variant="destructive">
            <AlertTitle>登录失败</AlertTitle>
            <AlertDescription>{{ errorMsg }}</AlertDescription>
          </Alert>

          <FieldGroup class="gap-4">
            <Field>
              <FieldLabel for="login-username">用户名</FieldLabel>
              <div class="relative">
                <UserIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
                <Input id="login-username" v-model="username" type="text" class="pl-9" placeholder="请输入用户名" required />
              </div>
            </Field>

            <Field>
              <FieldLabel for="login-password">密码</FieldLabel>
              <div class="relative">
                <LockIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
                <Input id="login-password" v-model="password" type="password" class="pl-9" placeholder="请输入密码" required />
              </div>
            </Field>
          </FieldGroup>

          <Button type="submit" class="mt-2 w-full" :disabled="isLoading">
            <Spinner v-if="isLoading" data-icon="inline-start" />
            {{ isLoading ? '登录中…' : '立即登入' }}
          </Button>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
