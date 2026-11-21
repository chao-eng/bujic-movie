<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  CircleCheckIcon,
  FlaskConicalIcon,
  PencilIcon,
  PlusIcon,
  PowerIcon,
  RefreshCwIcon,
  StarIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from 'lucide-vue-next'
import { useConfirm } from '@/composables/useConfirm'
import SearchModal from '@/components/search/SearchModal.vue'
import { EMPTY_SELECT_VALUE, fromSelectValue, toSelectValue } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  searchApi,
  SOURCE_CATEGORY_PRESETS,
  newznabLabel,
  type DownloadClientItem,
  type CategoryMapping,
  type ClientCategory,
} from '@/api/search'

const clients = ref<DownloadClientItem[]>([])
const isLoading = ref(false)
const testing = ref<Record<number, boolean>>({})
const { confirm } = useConfirm()

const showForm = ref(false)
const editing = ref<DownloadClientItem | null>(null)
const mappings = ref<CategoryMapping[]>([])
const isSaving = ref(false)

const form = reactive({
  name: '',
  host: '',
  port: 8080,
  use_ssl: false,
  url_base: '',
  api_key: '',
  username: '',
  password: '',
  default_category: '',
  initial_state: 'start',
  sequential: false,
  first_last_first: false,
  content_layout: 'original',
  priority: 50,
  is_default: false,
})

// Categories that already exist inside the selected download client. They are
// read live so the mapping targets real names instead of guesses.
const clientCategories = ref<ClientCategory[]>([])
const isLoadingCategories = ref(false)
const categoriesError = ref('')

const load = async () => {
  isLoading.value = true
  try {
    const res: any = await searchApi.listClients()
    if (res.code === 0) clients.value = res.data.items ?? []
  } catch {
    toast.error('读取下载器失败')
  } finally {
    isLoading.value = false
  }
}

const openCreate = () => {
  editing.value = null
  mappings.value = []
  clientCategories.value = []
  categoriesError.value = ''
  Object.assign(form, {
    name: '',
    host: '',
    port: 8080,
    use_ssl: false,
    url_base: '',
    api_key: '',
    username: '',
    password: '',
    default_category: '',
    initial_state: 'start',
    sequential: false,
    first_last_first: false,
    content_layout: 'original',
    priority: 50,
    is_default: false,
  })
  showForm.value = true
}

const openEdit = (client: DownloadClientItem) => {
  editing.value = client
  mappings.value = (client.category_mappings ?? []).map((m) => ({ ...m }))
  Object.assign(form, {
    name: client.name,
    host: client.host,
    port: client.port,
    use_ssl: client.use_ssl,
    url_base: client.url_base,
    api_key: '',
    username: client.username,
    password: '',
    default_category: client.default_category,
    initial_state: client.initial_state,
    sequential: client.sequential,
    first_last_first: client.first_last_first,
    content_layout: client.content_layout,
    priority: client.priority,
    is_default: client.is_default,
  })
  showForm.value = true
  loadClientCategories(client.id)
}

const addMapping = () => {
  // Preselect the first uncovered source category and, when the client already
  // has categories, the first free target — most mappings need no typing.
  const used = new Set(mappings.value.map((m) => m.source_category))
  const source = SOURCE_CATEGORY_PRESETS.find((p) => !used.has(p.value))?.value ?? 2000
  const target = clientCategories.value.find(
    (c) => !mappings.value.some((m) => m.client_category === c.name),
  )?.name ?? ''
  mappings.value.push({ source_category: source, client_category: target })
}

const loadClientCategories = async (clientId: number) => {
  clientCategories.value = []
  categoriesError.value = ''
  if (!clientId) return
  isLoadingCategories.value = true
  try {
    const res: any = await searchApi.clientCategories(clientId)
    if (res.code === 0) clientCategories.value = res.data.items ?? []
  } catch (err: any) {
    categoriesError.value = err?.msg || '读取下载器分类失败'
  } finally {
    isLoadingCategories.value = false
  }
}

const removeMapping = (index: number) => {
  mappings.value.splice(index, 1)
}

const save = async () => {
  if (!form.name.trim()) {
    toast.error('请填写名称')
    return
  }
  if (!form.host.trim()) {
    toast.error('请填写地址')
    return
  }
  const seen = new Set<number>()
  for (const mapping of mappings.value) {
    if (!mapping.client_category.trim()) {
      toast.error('分类映射的下载器分类不能为空')
      return
    }
    if (seen.has(mapping.source_category)) {
      toast.error(`来源分类 ${mapping.source_category} 重复配置`)
      return
    }
    seen.add(mapping.source_category)
  }

  isSaving.value = true
  try {
    const payload = {
      ...form,
      kind: 'qbittorrent',
      api_key: form.api_key,
      password: form.password,
      categories: mappings.value,
    }
    if (editing.value) {
      await searchApi.updateClient(editing.value.id, payload)
      toast.success('已保存')
    } else {
      await searchApi.createClient(payload)
      toast.success('已添加下载器')
    }
    showForm.value = false
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '保存失败')
  } finally {
    isSaving.value = false
  }
}

const testResult = ref<{ id: number; ok: boolean; message: string } | null>(null)

const test = async (client: DownloadClientItem) => {
  testing.value[client.id] = true
  testResult.value = null
  try {
    const res: any = await searchApi.testClient(client.id)
    const data = res.data ?? {}
    const message = `${res.msg || '连接正常'}${data.message ? '（' + data.message + '）' : ''}`
    testResult.value = { id: client.id, ok: true, message }
    toast.success(message)
    await load()
  } catch (err: any) {
    // 账号密码错、不可达、分类创建失败都在这里就地说明，不跳登录页。
    const message = err?.msg || '测试失败'
    testResult.value = { id: client.id, ok: false, message }
    toast.error(message)
    await load()
  } finally {
    testing.value[client.id] = false
  }
}

const toggleEnabled = async (client: DownloadClientItem) => {
  try {
    await searchApi.setClientEnabled(client.id, !client.enabled)
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '操作失败')
  }
}

const remove = async (client: DownloadClientItem) => {
  const ok = await confirm({
    title: '删除下载器',
    message: `删除「${client.name}」后，绑定它的站点将改为按优先级选择下载器。已创建的分类不会被删除。`,
    confirmText: '删除',
  })
  if (!ok) return
  try {
    await searchApi.deleteClient(client.id)
    toast.success('已删除')
    await load()
  } catch (err: any) {
    toast.error(err?.msg || '删除失败')
  }
}

const summary = computed(() => {
  const enabled = clients.value.filter((c) => c.enabled).length
  return `共 ${clients.value.length} 个下载器，启用 ${enabled} 个`
})

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="text-muted-foreground text-sm">{{ summary }}</p>
      <div class="ml-auto flex gap-2">
        <Button variant="outline" size="sm" :disabled="isLoading" @click="load">
          <Spinner v-if="isLoading" data-icon="inline-start" />
          <RefreshCwIcon v-else data-icon="inline-start" />
          刷新
        </Button>
        <Button size="sm" @click="openCreate">
          <PlusIcon data-icon="inline-start" />
          添加下载器
        </Button>
      </div>
    </div>

    <SearchModal
      v-if="showForm"
      :title="editing ? '编辑下载器' : '添加下载器'"
      subtitle="只支持 qBittorrent；保存目录由 qBittorrent 的分类规则决定，这里只配置分类"
      wide
      @close="showForm = false"
    >
      <FieldGroup class="gap-4">
        <div class="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel for="client-name">名称</FieldLabel>
            <Input id="client-name" v-model="form.name" placeholder="例如 NAS" />
          </Field>
          <Field>
            <FieldLabel for="client-host">地址</FieldLabel>
            <Input id="client-host" v-model="form.host" placeholder="192.168.1.10" />
          </Field>
          <Field>
            <FieldLabel for="client-port">端口</FieldLabel>
            <Input id="client-port" v-model.number="form.port" type="number" min="1" max="65535" />
          </Field>
          <Field>
            <FieldLabel for="client-url-base">路径前缀（反向代理时填）</FieldLabel>
            <Input id="client-url-base" v-model="form.url_base" placeholder="qbittorrent" />
          </Field>
          <Field orientation="horizontal" class="items-center">
            <Switch id="client-ssl" v-model="form.use_ssl" />
            <FieldLabel for="client-ssl">使用 HTTPS</FieldLabel>
          </Field>
          <Field>
            <FieldLabel for="client-priority">优先级（1 最高）</FieldLabel>
            <Input id="client-priority" v-model.number="form.priority" type="number" min="1" max="100" />
          </Field>

          <Field>
            <FieldLabel for="client-username">账号</FieldLabel>
            <Input id="client-username" v-model="form.username" placeholder="与 API Key 二选一" />
          </Field>
          <Field>
            <FieldLabel for="client-password">密码</FieldLabel>
            <Input
              id="client-password"
              v-model="form.password"
              type="password"
              :placeholder="editing?.password_mask ? '已保存，留空表示不修改' : '下载器密码'"
            />
            <FieldDescription v-if="editing?.password_mask" class="text-success">
              当前密码 {{ editing.password_mask }}；留空保存不会改动它
            </FieldDescription>
          </Field>
          <Field class="sm:col-span-2">
            <FieldLabel for="client-api-key">API Key（qB 5.x，可与账号密码二选一）</FieldLabel>
            <Input
              id="client-api-key"
              v-model="form.api_key"
              type="password"
              :placeholder="editing?.api_key_mask ? '已保存，留空表示不修改' : '留空表示使用账号密码'"
            />
          </Field>

          <Field>
            <FieldLabel for="client-default-category">默认分类（留空表示不传分类）</FieldLabel>
            <Input id="client-default-category" v-model="form.default_category" placeholder="bujic" />
          </Field>
          <Field>
            <FieldLabel for="client-initial-state">初始状态</FieldLabel>
            <Select v-model="form.initial_state">
              <SelectTrigger id="client-initial-state" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="start">开始下载</SelectItem>
                  <SelectItem value="pause">暂停</SelectItem>
                  <SelectItem value="force">强制开始</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel for="client-layout">内容布局</FieldLabel>
            <Select v-model="form.content_layout">
              <SelectTrigger id="client-layout" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="original">保持原样</SelectItem>
                  <SelectItem value="no-subfolder">不创建子目录</SelectItem>
                  <SelectItem value="subfolder">始终创建子目录</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <FieldContent class="gap-2">
            <Field orientation="horizontal">
              <Switch id="client-sequential" v-model="form.sequential" />
              <FieldLabel for="client-sequential">顺序下载</FieldLabel>
            </Field>
            <Field orientation="horizontal">
              <Switch id="client-first-last" v-model="form.first_last_first" />
              <FieldLabel for="client-first-last">首尾优先</FieldLabel>
            </Field>
            <Field orientation="horizontal">
              <Switch id="client-default" v-model="form.is_default" />
              <FieldLabel for="client-default">设为默认下载器</FieldLabel>
            </Field>
          </FieldContent>
        </div>

        <!-- Category mapping -->
        <Field class="border-border gap-3 rounded-lg border p-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div>
              <FieldLabel>分类映射</FieldLabel>
              <FieldDescription>
                把「资源属于哪一类」对应到「下载器里用哪个分类」。下载器按分类决定存到哪个目录，所以这里只选分类，不填路径。
              </FieldDescription>
            </div>
            <div class="flex items-center gap-1">
              <Button
                v-if="editing"
                variant="ghost"
                size="xs"
                :disabled="isLoadingCategories"
                @click="loadClientCategories(editing.id)"
              >
                <Spinner v-if="isLoadingCategories" data-icon="inline-start" />
                <RefreshCwIcon v-else data-icon="inline-start" />
                读取分类
              </Button>
              <Button variant="ghost" size="xs" @click="addMapping">
                <PlusIcon data-icon="inline-start" />
                添加映射
              </Button>
            </div>
          </div>

          <Alert v-if="categoriesError" class="text-destructive">
            <TriangleAlertIcon />
            <AlertTitle>读取分类失败</AlertTitle>
            <AlertDescription>{{ categoriesError }}</AlertDescription>
          </Alert>
          <FieldDescription v-else-if="editing && clientCategories.length" class="text-success">
            已读取 {{ clientCategories.length }} 个已有分类，可直接选择。
          </FieldDescription>
          <FieldDescription v-else-if="editing && !isLoadingCategories">
            点「读取分类」获取该下载器里已有的分类；也可以直接手填名字，测试时会自动创建。
          </FieldDescription>

          <FieldDescription v-if="!mappings.length">
            还没有映射。没有映射时，推送会使用下面的「默认分类」；默认分类也为空则由 qBittorrent 自己决定。
          </FieldDescription>

          <div class="flex flex-col gap-2">
            <div
              v-for="(mapping, index) in mappings"
              :key="index"
              class="bg-muted/40 flex flex-wrap items-center gap-2 rounded-lg px-2 py-1.5"
            >
              <Select v-model.number="mapping.source_category">
                <SelectTrigger size="sm" class="w-40"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem v-for="preset in SOURCE_CATEGORY_PRESETS" :key="preset.value" :value="preset.value">{{ preset.label }}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <span class="text-muted-foreground text-xs" aria-hidden="true">→</span>
              <Select
                v-if="clientCategories.length"
                :model-value="toSelectValue(mapping.client_category)"
                @update:model-value="mapping.client_category = fromSelectValue($event)"
              >
                <SelectTrigger size="sm" class="min-w-40"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem :value="EMPTY_SELECT_VALUE">（不指定）</SelectItem>
                    <SelectItem v-for="cat in clientCategories" :key="cat.name" :value="cat.name">{{ cat.name }}{{ cat.save_path ? ' — ' + cat.save_path : '' }}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Input v-else v-model="mapping.client_category" class="h-7 max-w-40" placeholder="下载器分类名" />
              <Button variant="ghost" size="icon-xs" aria-label="删除映射" @click="removeMapping(index)">
                <Trash2Icon />
              </Button>
            </div>
          </div>
        </Field>

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="showForm = false">取消</Button>
          <Button :disabled="isSaving" @click="save">
            <Spinner v-if="isSaving" data-icon="inline-start" />
            保存
          </Button>
        </div>
      </FieldGroup>
    </SearchModal>

    <Alert v-if="testResult" :class="testResult.ok ? 'text-success' : 'text-destructive'">
      <CircleCheckIcon v-if="testResult.ok" />
      <TriangleAlertIcon v-else />
      <AlertTitle>{{ testResult.ok ? '连接正常' : '测试失败' }}</AlertTitle>
      <AlertDescription>{{ testResult.message }}</AlertDescription>
    </Alert>

    <div v-if="isLoading" class="border-border flex items-center justify-center rounded-lg border px-4 py-10">
      <Spinner class="text-muted-foreground" />
    </div>

    <Empty v-else-if="!clients.length">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <PlusIcon />
        </EmptyMedia>
        <EmptyTitle>还没有下载器</EmptyTitle>
        <EmptyDescription>添加一个 qBittorrent，搜索到的资源才能一键下载。</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button @click="openCreate">
          <PlusIcon data-icon="inline-start" />
          添加下载器
        </Button>
      </EmptyContent>
    </Empty>

    <div v-else class="border-border divide-border divide-y overflow-hidden rounded-lg border">
      <div
        v-for="client in clients"
        :key="client.id"
        class="hover:bg-muted/50 flex flex-wrap items-start justify-between gap-3 p-3 transition-colors"
      >
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm">{{ client.name }}</span>
            <Badge :variant="client.enabled ? 'secondary' : 'outline'" :class="client.enabled ? 'text-success' : ''">
              {{ client.enabled ? '启用' : '停用' }}
            </Badge>
            <Badge v-if="client.is_default" variant="secondary" class="text-warning">
              <StarIcon data-icon="inline-start" />
              默认
            </Badge>
            <Badge v-if="client.unavailable_remaining_ms > 0" variant="secondary" class="text-warning">
              临时不可用 {{ Math.ceil(client.unavailable_remaining_ms / 60000) }} 分钟
            </Badge>
          </div>
          <div class="text-muted-foreground mt-1 flex flex-wrap gap-x-3 text-[11px]">
            <span>{{ client.host }}:{{ client.port }}{{ client.use_ssl ? '（HTTPS）' : '' }}</span>
            <span>优先级 {{ client.priority }}</span>
            <span>默认分类 {{ client.default_category || '未设置' }}</span>
            <span v-if="(client.category_mappings ?? []).length">
              映射 {{ (client.category_mappings ?? []).length }} 条：
              {{ (client.category_mappings ?? []).map((m) => `${newznabLabel(m.source_category)} → ${m.client_category || '不指定'}`).join('，') }}
            </span>
            <span v-else>未配置映射</span>
            <span v-if="client.password_mask">密码 {{ client.password_mask }}</span>
            <span v-if="client.api_key_mask">API Key {{ client.api_key_mask }}</span>
          </div>
        </div>
        <div class="flex shrink-0 flex-wrap items-center gap-1">
          <Button variant="ghost" size="xs" :disabled="testing[client.id]" @click="test(client)">
            <Spinner v-if="testing[client.id]" data-icon="inline-start" />
            <FlaskConicalIcon v-else data-icon="inline-start" />
            测试
          </Button>
          <Button variant="ghost" size="xs" @click="toggleEnabled(client)">
            <PowerIcon data-icon="inline-start" />
            {{ client.enabled ? '停用' : '启用' }}
          </Button>
          <Button variant="ghost" size="icon-xs" aria-label="编辑" @click="openEdit(client)">
            <PencilIcon />
          </Button>
          <Button variant="ghost" size="icon-xs" aria-label="删除" @click="remove(client)">
            <Trash2Icon />
          </Button>
        </div>
      </div>
    </div>
  </div>
</template>
