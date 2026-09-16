<template>
  <div class="mcp-manager">
    <div class="row items-center q-gutter-sm q-mb-sm">
      <div class="text-subtitle2 text-weight-bold">MCP 服务</div>
      <q-space />
      <q-btn
        flat
        round
        dense
        class="app-icon-btn"
        icon="refresh"
        aria-label="刷新 MCP 服务"
        :disable="busy || loading"
        @click="refresh"
      >
        <q-tooltip>刷新</q-tooltip>
      </q-btn>
      <q-btn
        v-if="scope.kind !== 'session'"
        unelevated
        dense
        color="primary"
        class="app-command-btn app-on-primary"
        icon="add"
        label="添加服务"
        :disable="busy"
        @click="edit()"
      />
    </div>
    <p class="text-caption text-muted q-mt-none">
      {{
        scope.kind === 'global'
          ? '启用的服务默认在全部项目和卡片中可用。'
          : scope.kind === 'project'
            ? '同名项目配置覆盖全局服务；开关可单独设置。'
            : '默认跟随项目。关闭后不再发现或调用该服务，不影响其他卡片。'
      }}
    </p>
    <q-banner v-if="error" dense rounded class="quick-command-error" role="alert">{{
      error
    }}</q-banner>
    <q-linear-progress v-if="loading" indeterminate color="primary" />
    <div v-if="!loading && !services.length && !error" class="global-settings-empty">
      {{ scope.kind === 'session' ? '暂无 MCP 服务，请在全局或项目设置中添加。' : '暂无 MCP 服务' }}
    </div>
    <q-list v-else bordered separator class="rounded-borders">
      <q-item v-for="service in services" :key="service.name" class="mcp-service">
        <q-item-section avatar class="mcp-service__icon"
          ><q-icon name="extension" color="primary"
        /></q-item-section>
        <q-item-section class="mcp-service__info">
          <q-item-label class="mcp-service__name">{{ service.name }}</q-item-label>
          <q-item-label caption
            >{{ sourceLabel(service)
            }}<span v-if="service.definition">
              · {{ service.definition.transport === 'stdio' ? '本地命令' : 'HTTP' }}</span
            ></q-item-label
          >
          <q-item-label
            v-if="checks[service.name]"
            caption
            :class="checks[service.name]?.failed ? 'text-negative' : 'text-positive'"
            role="status"
            >{{ checks[service.name]?.text }}</q-item-label
          >
        </q-item-section>
        <div class="mcp-service__actions">
          <q-btn
            v-if="scope.kind !== 'session'"
            flat
            round
            dense
            class="app-icon-btn"
            icon="network_check"
            :aria-label="`检测 ${service.name}`"
            :loading="checking === service.name"
            :disable="busy"
            @click="check(service)"
            ><q-tooltip>检测连接</q-tooltip></q-btn
          >
          <q-btn
            v-if="scope.kind !== 'session'"
            flat
            round
            dense
            class="app-icon-btn"
            icon="edit"
            :aria-label="`配置 ${service.name}`"
            :disable="busy"
            @click="edit(service)"
            ><q-tooltip>{{
              service.source === scope.kind ? '编辑配置' : '配置项目覆盖'
            }}</q-tooltip></q-btn
          >
          <q-btn
            v-if="service.overridden"
            flat
            round
            dense
            class="app-icon-btn"
            :icon="scope.kind === 'global' ? 'delete_outline' : 'restart_alt'"
            :aria-label="`${scope.kind === 'global' ? '删除' : '移除本级设置'} ${service.name}`"
            :disable="busy"
            @click="removing = service"
            ><q-tooltip>{{
              scope.kind === 'global' ? '删除服务' : '移除本级设置，恢复继承'
            }}</q-tooltip></q-btn
          >
          <q-toggle
            :model-value="service.enabled"
            color="primary"
            :aria-label="`启用 ${service.name}`"
            :disable="busy"
            @update:model-value="toggle(service, $event)"
          />
        </div>
      </q-item>
    </q-list>

    <q-dialog v-model="editing" :persistent="busy">
      <q-card class="app-content-dialog mcp-editor">
        <q-card-section class="row items-center q-pb-sm"
          ><div class="text-subtitle1 text-weight-bold">
            {{ originalName ? '配置 MCP 服务' : '添加 MCP 服务' }}
          </div>
          <q-space /><q-btn
            v-close-popup
            flat
            round
            dense
            icon="close"
            class="app-icon-btn"
            aria-label="关闭 MCP 配置"
            :disable="busy"
        /></q-card-section>
        <q-separator />
        <q-form @submit="save">
          <q-card-section class="q-gutter-md">
            <q-banner v-if="editError" dense class="quick-command-error" role="alert">{{
              editError
            }}</q-banner>
            <q-input
              v-model="draft.name"
              outlined
              dense
              label="服务名称"
              hint="同名项目服务会覆盖全局配置"
              :readonly="Boolean(originalName)"
              :rules="[
                (v) =>
                  /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(v) ||
                  '使用 1–64 位字母、数字、下划线或短横线',
              ]"
            />
            <q-select
              v-model="draft.transport"
              outlined
              dense
              label="连接方式"
              emit-value
              map-options
              :options="[
                { label: '本地命令（stdio）', value: 'stdio' },
                { label: 'Streamable HTTP', value: 'http' },
              ]"
            />
            <template v-if="draft.transport === 'stdio'">
              <q-input
                v-model="draft.command"
                outlined
                dense
                label="启动命令"
                placeholder="npx"
                :rules="[(v) => !!v.trim() || '请填写启动命令']"
              />
              <q-input
                v-model="draft.args"
                outlined
                dense
                type="textarea"
                :rows="3"
                label="命令参数（每行一个）"
                placeholder="-y&#10;package-name"
              />
              <q-input
                v-model="draft.env"
                outlined
                dense
                type="textarea"
                :rows="3"
                label="环境变量（每行 NAME=value）"
                hint="凭据保存在服务端；保留圆点表示不修改已有值。"
              />
            </template>
            <template v-else>
              <q-input
                v-model="draft.url"
                outlined
                dense
                label="MCP 地址"
                placeholder="https://example.com/mcp"
                :rules="[(v) => /^https?:\/\//.test(v) || '请填写 HTTP/HTTPS 地址']"
              />
              <q-input
                v-model="draft.headers"
                outlined
                dense
                type="textarea"
                :rows="3"
                label="请求头（每行 Name=value）"
                hint="例如 Authorization=Bearer …；保留圆点表示不修改已有值。"
              />
            </template>
            <q-toggle v-model="draft.enabled" color="primary" label="默认启用" />
          </q-card-section>
          <q-card-actions align="right"
            ><q-btn v-close-popup flat label="取消" :disable="busy" /><q-btn
              type="submit"
              unelevated
              color="primary"
              class="app-on-primary"
              label="保存"
              :loading="busy"
          /></q-card-actions>
        </q-form>
      </q-card>
    </q-dialog>
    <q-dialog
      :model-value="Boolean(removing)"
      :persistent="busy"
      @update:model-value="!$event && (removing = null)"
    >
      <q-card class="app-content-dialog"
        ><q-card-section class="text-subtitle1">{{
          scope.kind === 'global' ? '删除服务' : '移除本级设置'
        }}</q-card-section
        ><q-card-section class="q-pt-none">{{
          scope.kind === 'global'
            ? `删除 ${removing?.name} 的全局配置？项目独立配置会保留。`
            : `移除 ${removing?.name} 的本级配置和开关，恢复上层设置？`
        }}</q-card-section
        ><q-card-actions align="right"
          ><q-btn v-close-popup flat label="取消" :disable="busy" /><q-btn
            flat
            color="negative"
            label="确认"
            :loading="busy"
            @click="remove" /></q-card-actions
      ></q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import {
  getMCPServices,
  saveMCPService,
  deleteMCPService,
  checkMCPService,
  type MCPScope,
  type MCPService,
  type MCPDefinition,
} from '@/services/mcp';
const props = defineProps<{ kind: MCPScope['kind']; scopeId?: string }>();
const scope = computed<MCPScope>(() => ({
  kind: props.kind,
  ...(props.scopeId ? { id: props.scopeId } : {}),
}));
const services = ref<MCPService[]>([]);
const loading = ref(false);
const busy = ref(false);
const error = ref('');
const editError = ref('');
const checks = ref<Record<string, { text: string; failed: boolean }>>({});
const checking = ref('');
const editing = ref(false);
const removing = ref<MCPService | null>(null);
const originalName = ref('');
const draft = ref<{
  name: string;
  transport: MCPDefinition['transport'];
  command: string;
  args: string;
  env: string;
  url: string;
  headers: string;
  enabled: boolean;
}>({
  name: '',
  transport: 'stdio',
  command: '',
  args: '',
  env: '',
  url: '',
  headers: '',
  enabled: true,
});
let request = 0;
async function refresh() {
  const current = ++request;
  loading.value = true;
  error.value = '';
  try {
    const data = await getMCPServices(scope.value);
    if (current === request) services.value = data;
  } catch (e) {
    if (current === request) error.value = message(e);
  } finally {
    if (current === request) loading.value = false;
  }
}
function message(e: unknown) {
  return e instanceof Error ? e.message : '操作失败，请重试';
}
function sourceLabel(s: MCPService) {
  const state = s.enabled ? '开启' : '关闭';
  if (scope.value.kind === 'global') return `默认${state}`;
  const source = s.source === 'global' ? '全局配置' : '项目配置';
  return `${source} · ${s.overridden ? (scope.value.kind === 'session' ? '本卡片' : '本项目') + state : `跟随默认（${state}）`}`;
}

function lines(value?: Record<string, string>) {
  return Object.entries(value ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join('\n');
}
function pairs(text: string) {
  const result: Record<string, string> = {};
  for (const line of text.split('\n')) {
    if (!line.trim()) continue;
    const index = line.indexOf('=');
    if (index < 1) throw new Error('环境变量和请求头须使用 NAME=value 格式');
    const name = line.slice(0, index).trim();
    if (!name || Object.hasOwn(result, name)) throw new Error('键名不能为空或重复');
    result[name] = line.slice(index + 1);
  }
  return result;
}
function edit(s?: MCPService) {
  originalName.value = s?.name ?? '';
  const d = s?.definition;
  draft.value = {
    name: s?.name ?? '',
    transport: d?.transport ?? 'stdio',
    command: d?.command ?? '',
    args: d?.args?.join('\n') ?? '',
    env: lines(d?.env),
    url: d?.url ?? '',
    headers: lines(d?.headers),
    enabled: s?.enabled ?? true,
  };
  editError.value = '';
  editing.value = true;
}
async function save() {
  busy.value = true;
  editError.value = '';
  try {
    const d = draft.value;
    const definition: MCPDefinition =
      d.transport === 'stdio'
        ? {
            transport: 'stdio',
            command: d.command.trim(),
            args: d.args.split('\n').filter((v) => v.length > 0),
            env: pairs(d.env),
          }
        : { transport: 'http', url: d.url.trim(), headers: pairs(d.headers) };
    await saveMCPService(scope.value, d.name.trim(), d.enabled, definition);
    editing.value = false;
    checks.value = {};
    await refresh();
  } catch (e) {
    editError.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function toggle(s: MCPService, enabled: boolean) {
  busy.value = true;
  error.value = '';
  try {
    await saveMCPService(scope.value, s.name, enabled);
    await refresh();
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function remove() {
  if (!removing.value) return;
  busy.value = true;
  try {
    await deleteMCPService(scope.value, removing.value.name);
    removing.value = null;
    await refresh();
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function check(s: MCPService) {
  checking.value = s.name;
  busy.value = true;
  try {
    const count = await checkMCPService(scope.value, s.name);
    checks.value[s.name] = { text: `连接成功 · ${count} 个工具`, failed: false };
  } catch (e) {
    checks.value[s.name] = { text: message(e), failed: true };
  } finally {
    busy.value = false;
    checking.value = '';
  }
}
watch(
  scope,
  () => {
    services.value = [];
    checks.value = {};
    void refresh();
  },
  { immediate: true },
);
</script>

<style scoped>
.mcp-manager {
  min-width: 0;
}
.mcp-service {
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
}
.mcp-service__icon {
  min-width: 32px;
  padding-right: 4px;
}
.mcp-service__info {
  min-width: 120px;
}
.mcp-service__name {
  overflow-wrap: anywhere;
}
.mcp-service__actions {
  display: flex;
  align-items: center;
  margin-left: auto;
}
.mcp-editor {
  width: 560px;
  max-width: calc(100vw - 24px);
}
</style>
