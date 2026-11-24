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
  type ClientKind,
  type ClientKindCapabilities,
} from '@/api/search'

const clients = ref<DownloadClientItem[]>([])
const kinds = ref<ClientKind[]>([])
const isLoading = ref(false)
const testing = ref<Record<number, boolean>>({})
const { confirm } = useConfirm()

const showForm = ref(false)
const editing = ref<DownloadClientItem | null>(null)
const mappings = ref<CategoryMapping[]>([])
const isSaving = ref(false)

const emptyForm = {
  kind: 'qbittorrent',
  name: '',
  host: '',
  port: 8080,
  use_ssl: false,
  url_base: '',
  api_key: '',
  username: '',
  password: '',
  default_category: '',
  default_save_path: '',
  initial_state: 'start',
  sequential: false,
  first_last_first: false,
  content_layout: 'original',
  priority: 50,
  is_default: false,
}

const form = reactive({ ...emptyForm })

// Capabilities decide which fields are even shown: asking for an API key on
// Transmission or a category on Aria2 only invites wrong input.
const selectedKind = computed<ClientKind | undefined>(() => kinds.value.find((k) => k.kind === form.kind))
const caps = computed<ClientKindCapabilities>(
  () =>
    selectedKind.value?.capabilities ?? {
      categories: true,
      category_path: true,
      save_path: true,
      queue: true,
      content_layout: true,
    },
)
// A download directory is the only way to pick a folder whenever the client
// cannot derive one from a category.
const needsSavePath = computed(() => !caps.value.category_path)
// Only a client without any category feature *requires* one; Deluge keeps a
// usable default directory of its own, so there the field stays optional.
const requiresSavePath = computed(() => !caps.value.categories)
const authMode = computed(() => selectedKind.value?.auth ?? 'api_key_or_user_pass')
const needsCategory = computed(() => caps.value.categories)

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

const loadKinds = async () => {
  try {
    const res: any = await searchApi.clientKinds()
    if (res.code === 0) kinds.value = res.data.items ?? []
  } catch {
    // Fall back to qBittorrent only; the form still works for the common case.
    kinds.value = []
  }
}

const kindName = (kind: string) => kinds.value.find((k) => k.kind === kind)?.name ?? kind

// Switching kind resets the port and URL base to that client's defaults, so a
// half-filled qBittorrent form does not silently point at 8080 on Transmission.
const applyKindDefaults = (kind: string) => {
  const info = kinds.value.find((k) => k.kind === kind)
  if (!info) return
  form.port = info.default_port
  form.url_base = info.default_url_base
}

const openCreate = () => {
  editing.value = null
  mappings.value = []
  clientCategories.value = []
  categoriesError.value = ''
  Object.assign(form, { ...emptyForm })
  applyKindDefaults(form.kind)
  showForm.value = true
}

const openEdit = (client: DownloadClientItem) => {
  editing.value = client
  mappings.value = (client.category_mappings ?? []).map((m) => ({ ...m }))
  Object.assign(form, {
    kind: client.kind || 'qbittorrent',
    name: client.name,
    host: client.host,
    port: client.port,
    use_ssl: client.use_ssl,
    url_base: client.url_base,
    api_key: '',
    username: client.username,
    password: '',
    default_category: client.default_category,
    default_save_path: client.default_save_path ?? '',
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
  mappings.value.push({ source_category: source, client_category: target, save_path: '' })
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
    const category = (mapping.client_category ?? '').trim()
    const savePath = (mapping.save_path ?? '').trim()
    if (!category && !savePath) {
      toast.error('每条映射至少填下载器分类或下载目录')
      return
    }
    if (requiresSavePath.value && !savePath) {
      toast.error(`${kindName(form.kind)} 不能按分类自动落盘，请为「${newznabLabel(mapping.source_category)}」填写下载目录`)
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
      // Switching kind inside one form session can leave a directory behind on
      // this client. Drop it for kinds that cannot use one, so the request does
      // not carry a setting the backend would reject or ignore.
      default_save_path: caps.value.save_path ? form.default_save_path : '',
      categories: mappings.value.map((m) => ({
        ...m,
        save_path: caps.value.save_path ? (m.save_path ?? '').trim() : '',
      })),
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

onMounted(() => {
  loadKinds()
  load()
})
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
      :subtitle="`${kindName(form.kind)}${requiresSavePath ? '：该下载器没有分类，请在映射里指定下载目录' : needsSavePath ? '：分类只是标签、不决定目录，需按分类指定下载目录' : '：分类自带保存目录，目录随分类走'}`"
      wide
      @close="showForm = false"
    >
      <FieldGroup class="gap-4">
        <div class="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel for="client-kind">类型</FieldLabel>
            <Select v-model="form.kind" @update:model-value="(value) => applyKindDefaults(String(value ?? ''))">
              <SelectTrigger id="client-kind" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem v-for="info in kinds" :key="info.kind" :value="info.kind">{{ info.name }}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <FieldDescription>
              不同下载器的认证方式与分类能力不同，表单会按类型显示对应字段。
            </FieldDescription>
          </Field>
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
            <Input id="client-url-base" v-model="form.url_base" :placeholder="selectedKind?.default_url_base || 'qbittorrent'" />
          </Field>
          <Field orientation="horizontal" class="items-center">
            <Switch id="client-ssl" v-model="form.use_ssl" />
            <FieldLabel for="client-ssl">使用 HTTPS</FieldLabel>
          </Field>
          <Field>
            <FieldLabel for="client-priority">优先级（1 最高）</FieldLabel>
            <Input id="client-priority" v-model.number="form.priority" type="number" min="1" max="100" />
          </Field>

          <Field v-if="authMode === 'api_key_or_user_pass' || authMode === 'user_pass'">
            <FieldLabel for="client-username">账号</FieldLabel>
            <Input
              id="client-username"
              v-model="form.username"
              :placeholder="authMode === 'api_key_or_user_pass' ? '与 API Key 二选一' : 'Transmission 账号'"
            />
          </Field>
          <Field v-if="authMode !== 'token'">
            <FieldLabel for="client-password">{{ authMode === 'password' ? 'Web UI 密码' : '密码' }}</FieldLabel>
            <Input
              id="client-password"
              v-model="form.password"
              type="password"
              :placeholder="editing?.password_mask ? '已保存，留空表示不修改' : authMode === 'password' ? 'Deluge Web UI 密码' : '下载器密码'"
            />
            <FieldDescription v-if="editing?.password_mask" class="text-success">
              当前密码 {{ editing.password_mask }}；留空保存不会改动它
            </FieldDescription>
          </Field>
          <Field v-if="authMode === 'token'">
            <FieldLabel for="client-password">RPC 密钥（未设置可留空）</FieldLabel>
            <Input
              id="client-password"
              v-model="form.password"
              type="password"
              :placeholder="editing?.password_mask ? '已保存，留空表示不修改' : 'Aria2 --rpc-secret 的值'"
            />
          </Field>
          <Field v-if="authMode === 'api_key_or_user_pass'" class="sm:col-span-2">
            <FieldLabel for="client-api-key">API Key（qB 5.x，可与账号密码二选一）</FieldLabel>
            <Input
              id="client-api-key"
              v-model="form.api_key"
              type="password"
              :placeholder="editing?.api_key_mask ? '已保存，留空表示不修改' : '留空表示使用账号密码'"
            />
          </Field>

          <Field v-if="needsCategory">
            <FieldLabel for="client-default-category">默认分类（留空表示不传分类）</FieldLabel>
            <Input id="client-default-category" v-model="form.default_category" placeholder="bujic" />
          </Field>
          <Field v-if="caps.save_path">
            <FieldLabel for="client-default-save-path">
              {{ requiresSavePath ? '默认下载目录' : '默认下载目录（可选）' }}
            </FieldLabel>
            <Input
              id="client-default-save-path"
              v-model="form.default_save_path"
              placeholder="/downloads"
            />
            <FieldDescription>
              <template v-if="needsSavePath">
                {{ kindName(form.kind) }} 不会按分类选目录；没有匹配映射时用这里。留空则用下载器自己的默认目录。
              </template>
              <template v-else>
                没有匹配映射时用这里；留空则由下载器决定。
              </template>
            </FieldDescription>
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
          <Field v-if="caps.content_layout">
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
            <Field v-if="caps.queue" orientation="horizontal">
              <Switch id="client-sequential" v-model="form.sequential" />
              <FieldLabel for="client-sequential">顺序下载</FieldLabel>
            </Field>
            <Field v-if="caps.queue" orientation="horizontal">
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
                <template v-if="requiresSavePath">
                  把「资源属于哪一类」对应到「下载到哪个目录」。{{ kindName(form.kind) }} 没有分类，所以这里必须填目录。
                </template>
                <template v-else-if="needsSavePath">
                  把「资源属于哪一类」对应到「下载器里的哪个标签」和「下载到哪个目录」。
                  {{ kindName(form.kind) }} 的标签不影响目录，目录由这里决定。
                </template>
                <template v-else>
                  把「资源属于哪一类」对应到「下载器里用哪个分类」。
                  {{ kindName(form.kind) }} 的分类自带保存目录，目录随分类走，所以这里不填路径。
                </template>
              </FieldDescription>
            </div>
            <div class="flex items-center gap-1">
              <Button
                v-if="editing && needsCategory"
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
          <FieldDescription v-else-if="editing && needsCategory && clientCategories.length" class="text-success">
            已读取 {{ clientCategories.length }} 个已有分类，可直接选择。
          </FieldDescription>
          <FieldDescription v-else-if="editing && needsCategory && !isLoadingCategories">
            点「读取分类」获取该下载器里已有的分类；也可以直接手填名字，测试时会自动创建。
          </FieldDescription>

          <FieldDescription v-if="!mappings.length">
            <template v-if="needsSavePath">
              还没有映射。没有映射时，推送会使用「默认下载目录」；也为空则由 {{ kindName(form.kind) }} 自己决定。
            </template>
            <template v-else>
              还没有映射。没有映射时，推送会使用「默认分类」；默认分类也为空则交给下载器决定保存位置。
            </template>
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
              <template v-if="needsCategory">
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
              </template>
              <Input
                v-if="caps.save_path"
                v-model="mapping.save_path"
                class="h-7 max-w-56"
                :placeholder="requiresSavePath || needsSavePath ? '下载目录' : '下载目录（可选）'"
              />
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
        <EmptyDescription>添加一个下载器（qBittorrent / Transmission / Deluge / Aria2），搜索到的资源才能一键下载。</EmptyDescription>
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
            <span>{{ kindName(client.kind) }}</span>
            <span>{{ client.host }}:{{ client.port }}{{ client.use_ssl ? '（HTTPS）' : '' }}</span>
            <span>优先级 {{ client.priority }}</span>
            <span v-if="client.capabilities?.categories !== false">默认分类 {{ client.default_category || '未设置' }}</span>
            <span v-if="client.default_save_path">默认目录 {{ client.default_save_path }}</span>
            <span v-if="(client.category_mappings ?? []).length">
              映射 {{ (client.category_mappings ?? []).length }} 条：
              {{ (client.category_mappings ?? []).map((m) => `${newznabLabel(m.source_category)} → ${m.save_path || m.client_category || '不指定'}`).join('，') }}
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
