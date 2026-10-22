<script setup>
import { computed, onMounted, ref } from 'vue'
import { ExportServerDeploymentBundle, GetConfig, GetModels, GetDetectionInfo, OpenPathInFileManager, UpdateConfig } from '../../wailsjs/go/main/App.js'

const emit = defineEmits(['log', 'status-refresh'])

const config = ref({})
const models = ref([])
const detection = ref(null)
const saving = ref(false)
const exportingBundle = ref(false)
const bundlePickPolicy = ref('auto')
const bundleResult = ref(null)
const openSelect = ref('')
const fallbackModelsText = ref('')
const isIPCBackend = computed(() => (config.value.Backend || 'ipc') === 'ipc')
const formattedTokenExpireAt = computed(() => formatDateTime(detection.value?.remoteTokenExpireAt))
const remoteProxyStatus = computed(() => {
  if (!detection.value) return ''
  if (detection.value.remoteProxyError) return detection.value.remoteProxyError
  if (detection.value.remoteProxyUrl) {
    return `${detection.value.remoteProxyUrl}（${detection.value.remoteProxySource || '显式配置'}）`
  }
  return '未显式配置；如存在 HTTP_PROXY / HTTPS_PROXY，Go 默认传输仍会尊重系统环境变量'
})

const selectOptions = {
  Backend: [
    { value: 'ipc', label: 'IPC 插件' },
    { value: 'remote', label: '远端 API' },
  ],
  Transport: [
    { value: 'auto', label: '自动' },
    { value: 'pipe', label: 'Socket / Named Pipe' },
    { value: 'websocket', label: 'WebSocket' },
  ],
  Mode: [
    { value: 'agent', label: 'Agent' },
    { value: 'chat', label: 'Chat' },
  ],
  ShellType: [
    { value: 'zsh', label: 'zsh' },
    { value: 'bash', label: 'bash' },
    { value: 'powershell', label: 'PowerShell' },
    { value: 'cmd', label: 'cmd' },
  ],
  SessionMode: [
    { value: 'auto', label: '自动' },
    { value: 'reuse', label: '复用' },
    { value: 'fresh', label: '每次新建' },
  ],
  RemoteAuthPick: [
    { value: 'auto', label: '默认优先级' },
    { value: 'newest', label: '最新缓存' },
    { value: 'longest', label: '最长有效期' },
  ],
}

const selectLabel = computed(() => (field) => {
  const value = field === 'RemoteAuthPick' ? bundlePickPolicy.value : config.value[field]
  const option = selectOptions[field]?.find((item) => item.value === value)
  return option?.label || '请选择'
})

function randomKey() { return Array.from(crypto.getRandomValues(new Uint8Array(32)),v=>v.toString(16).padStart(2,'0')).join('') }
function toggleSelect(field) {
  openSelect.value = openSelect.value === field ? '' : field
}

function chooseOption(field, value) {
  if (field === 'RemoteAuthPick') {
    bundlePickPolicy.value = value
  } else {
    config.value[field] = value
  }
  openSelect.value = ''
  if (field !== 'RemoteAuthPick') {
    refreshDetection()
  }
}

function formatDateTime(value) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

onMounted(async () => {
  try {
    config.value = await GetConfig()
    models.value = await GetModels().catch(() => [])
    fallbackModelsText.value = Array.isArray(config.value.RemoteFallbackModels)
      ? config.value.RemoteFallbackModels.join('\n')
      : ''
    await refreshDetection()
  } catch (e) {
    emit('log', 'error', '配置加载失败：' + (e.message || String(e)))
  }
})

async function refreshDetection() {
  try {
    detection.value = await GetDetectionInfo()
  } catch (e) {
    emit('log', 'warn', '探测信息加载失败：' + (e.message || String(e)))
  }
}

async function save() {
  saving.value = true
  try {
    config.value.RemoteFallbackModels = fallbackModelsText.value
      .split(/\n|,/)
      .map((item) => item.trim())
      .filter(Boolean)
    await UpdateConfig(config.value)
    await refreshDetection()
    emit('log', 'info', '配置已保存，代理已按需重启')
    emit('status-refresh')
  } catch (e) {
    emit('log', 'error', '配置保存失败：' + (e.message || String(e)))
  } finally {
    saving.value = false
  }
}

async function exportServerBundle() {
  exportingBundle.value = true
  bundleResult.value = null
  try {
    const result = await ExportServerDeploymentBundle({ pickPolicy: bundlePickPolicy.value })
    if (result?.zipPath) {
      bundleResult.value = result
      emit('log', 'info', '服务器部署包已导出：' + result.zipPath)
    }
  } catch (e) {
    emit('log', 'error', '服务器部署包导出失败：' + (e.message || String(e)))
  } finally {
    exportingBundle.value = false
  }
}

async function openBundleFolder() {
  if (!bundleResult.value?.zipPath) return
  try {
    await OpenPathInFileManager(bundleResult.value.zipPath)
  } catch (e) {
    emit('log', 'warn', '打开部署包目录失败：' + (e.message || String(e)))
  }
}
</script>

<template>
  <div class="page">
    <div class="page-title">
      <div>
        <h1>设置</h1>
        <p>配置监听地址、Lingma / QoderCN 传输方式、模型探测超时、会话复用和请求超时。</p>
      </div>
      <button class="primary-button" type="button" :disabled="saving" @click="save">
        {{ saving ? '保存中...' : '保存并重启' }}
      </button>
    </div>

    <section class="glass-panel">
 <h2>访问与日志</h2>
 <p>本机监听可留空密钥；局域网监听必须设置访问密钥。管理密钥只用于调试接口，未设置时调试仅限本机。</p>
 <div class="form-grid">
  <div class="field"><label>代理访问密钥</label><input v-model="config.APIKey" type="password" autocomplete="new-password" /><button class="secondary-button" type="button" @click="config.APIKey=randomKey()">生成随机密钥</button></div>
  <div class="field"><label>管理密钥（可选）</label><input v-model="config.AdminKey" type="password" autocomplete="new-password" /><button class="secondary-button" type="button" @click="config.AdminKey=randomKey()">生成管理密钥</button></div>
  <div class="field"><label>浏览器允许来源（每行一个完整 Origin）</label><textarea :value="(config.AllowedOrigins || []).join('\n')" @input="config.AllowedOrigins=$event.target.value.split(/\n/).map(v=>v.trim()).filter(Boolean)" placeholder="http://localhost:3000" /></div>
  <div class="field"><label>正文记录</label><select v-model="config.LogMode"><option value="metadata">仅元数据</option><option value="errors">仅失败正文（默认）</option><option value="full">完整正文（受容量限制）</option></select><small>统计始终保留；修改并保存后也会清理已有正文。</small></div>
  <div class="field"><label>单条正文上限（字节）</label><input v-model.number="config.LogBodyMaxBytes" type="number" min="1024" max="1048576" /></div>
  <div class="field"><label>历史记录总上限（字节）</label><input v-model.number="config.LogMaxBytes" type="number" min="1048576" max="134217728" /></div>
  <div class="field"><label>记录保留天数</label><input v-model.number="config.LogRetentionDays" type="number" min="1" max="365" /></div>
  <div class="field"><label>请求体上限（字节，包含图片）</label><input v-model.number="config.MaxRequestBytes" type="number" min="1024" max="134217728" /></div>
  <div class="field"><label>排队等待上限（秒）</label><input v-model.number="config.QueueTimeoutSeconds" type="number" min="1" max="600" /></div>
 </div>
</section>
<section class="grid-2 settings-grid">
      <div class="glass-panel">
        <div class="panel-header">
          <div>
            <h2>服务监听</h2>
            <p>第三方客户端连接本地代理使用这组地址。</p>
          </div>
        </div>
        <div class="form-grid">
          <div class="field">
            <label>连接模式</label>
            <div class="custom-select" :class="{ open: openSelect === 'Backend' }">
              <button type="button" @click="toggleSelect('Backend')">
                <span>{{ selectLabel('Backend') }}</span>
                <i class="bi bi-chevron-down" aria-hidden="true"></i>
              </button>
              <div v-if="openSelect === 'Backend'" class="select-menu">
                <button
                  v-for="option in selectOptions.Backend"
                  :key="option.value"
                  :class="{ selected: option.value === config.Backend }"
                  type="button"
                  @click="chooseOption('Backend', option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
          </div>
          <div class="field">
            <label>主机</label>
            <input v-model="config.Host" type="text" placeholder="127.0.0.1" />
          </div>
          <div class="field">
            <label>端口</label>
            <input v-model.number="config.Port" type="number" placeholder="8095" />
          </div>
          <div class="field">
            <label>Lingma / QoderCN 传输方式</label>
            <div class="custom-select" :class="{ open: openSelect === 'Transport' }">
              <button type="button" @click="toggleSelect('Transport')">
                <span>{{ selectLabel('Transport') }}</span>
                <i class="bi bi-chevron-down" aria-hidden="true"></i>
              </button>
              <div v-if="openSelect === 'Transport'" class="select-menu">
                <button
                  v-for="option in selectOptions.Transport"
                  :key="option.value"
                  :class="{ selected: option.value === config.Transport }"
                  type="button"
                  @click="chooseOption('Transport', option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
          </div>
          <div class="field">
            <label>超时秒数</label>
            <input v-model.number="config.Timeout" type="number" min="0" />
            <small>0 表示不设置代理层单次请求超时，适合长流程任务。</small>
          </div>
          <div class="field">
            <label>探测超时秒数</label>
            <input v-model.number="config.WarmupTimeout" type="number" min="1" placeholder="30" />
            <small>用于启动代理和手动探测模型时的 warmup 超时，默认 30 秒。</small>
          </div>
          <div class="field span-2 switch-field">
            <div>
              <label for="auto-start-on-boot">开机自动启动</label>
              <p>登录系统后自动启动桌面代理，保存设置后生效。</p>
            </div>
            <label class="switch">
              <input id="auto-start-on-boot" v-model="config.AutoStartOnBoot" type="checkbox" />
              <span></span>
            </label>
          </div>
          <div class="field span-2 switch-field">
            <div>
              <label>远端超时兜底</label>
              <p>设置正数超时后，远端 API 超时、限流或 5xx 且尚未流式输出时，自动切换到下一个可用模型。</p>
            </div>
            <label class="switch">
              <input v-model="config.RemoteFallbackEnabled" type="checkbox" />
              <span></span>
            </label>
          </div>
          <div class="field span-2">
            <label>兜底模型顺序</label>
            <textarea
              v-model="fallbackModelsText"
              placeholder="kmodel&#10;mmodel&#10;dashscope_qwen3_coder&#10;dashscope_qmodel"
            ></textarea>
          </div>
          <div class="field span-2">
            <label>WebSocket 地址</label>
            <input v-model="config.WebSocketURL" type="text" placeholder="留空自动探测 Lingma / QoderCN WebSocket" />
          </div>
          <div class="field span-2">
            <label>Socket / Named Pipe</label>
            <input v-model="config.Pipe" type="text" placeholder="留空自动探测 macOS Socket / Windows Named Pipe" />
          </div>
          <div class="field span-2">
            <label>远端 API 域名</label>
            <input v-model="config.RemoteBaseURL" type="text" placeholder="留空自动探测，默认 https://lingma.alibabacloud.com" />
          </div>
          <div class="field span-2">
            <label>远端代理地址</label>
            <input v-model="config.RemoteProxyURL" type="text" placeholder="可选，例如 http://127.0.0.1:7890" />
            <small>仅影响远端 API 上游 HTTP 请求，不影响本地 IPC WebSocket / Named Pipe。</small>
          </div>
          <div class="field span-2">
            <label>远端认证文件</label>
            <input v-model="config.RemoteAuthFile" type="text" placeholder="可选 credentials.json；留空只读本机 Lingma / QoderCN 登录缓存" />
          </div>
          <div class="field span-2">
            <label>远端 Cosy 版本</label>
            <input v-model="config.RemoteVersion" type="text" placeholder="默认 2.11.2" />
          </div>
        </div>
        <div class="hint-box">
          <strong>自动探测失败时</strong>
          <span>IPC 模式先确认 Lingma / QoderCN 已启动并登录。远端 API 模式会优先读取认证文件；留空时只读本机 Lingma / QoderCN 登录缓存，不会写入或上传登录态。</span>
        </div>
        <div class="deploy-box">
          <div>
            <strong>服务器部署包</strong>
            <span>导出 credentials.json、配置和 docker-compose.yml；压缩包包含登录密钥，请只发到自己的服务器。</span>
          </div>
          <div class="deploy-actions">
            <div class="custom-select compact-select" :class="{ open: openSelect === 'RemoteAuthPick' }">
              <button type="button" @click="toggleSelect('RemoteAuthPick')">
                <span>{{ selectLabel('RemoteAuthPick') }}</span>
                <i class="bi bi-chevron-down" aria-hidden="true"></i>
              </button>
              <div v-if="openSelect === 'RemoteAuthPick'" class="select-menu">
                <button
                  v-for="option in selectOptions.RemoteAuthPick"
                  :key="option.value"
                  :class="{ selected: option.value === bundlePickPolicy }"
                  type="button"
                  @click="chooseOption('RemoteAuthPick', option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <button class="secondary-button" type="button" :disabled="exportingBundle" @click="exportServerBundle">
              {{ exportingBundle ? '导出中...' : '导出部署包' }}
            </button>
          </div>
          <div v-if="bundleResult?.zipPath" class="bundle-result">
            <div>
              <span>文件</span>
              <code>{{ bundleResult.zipPath }}</code>
            </div>
            <div>
              <span>凭据</span>
              <code>{{ bundleResult.userId || '未知用户' }} · {{ bundleResult.machineId || '未知机器' }}</code>
            </div>
            <div v-if="bundleResult.tokenExpireAt">
              <span>有效期</span>
              <code :class="{ 'warn-text': bundleResult.tokenExpired }">{{ formatDateTime(bundleResult.tokenExpireAt) }}{{ bundleResult.tokenExpired ? '（已过期）' : '' }}</code>
            </div>
            <button class="text-button" type="button" @click="openBundleFolder">打开目录</button>
          </div>
        </div>
        <div v-if="detection" class="detect-card">
          <div class="detect-title">
            <strong>当前解析结果</strong>
            <button type="button" @click="refreshDetection">刷新</button>
          </div>
          <dl>
            <div>
              <dt>监听地址</dt>
              <dd>{{ detection.listenUrl || '未启动' }}</dd>
            </div>
            <div>
              <dt>当前后端</dt>
              <dd>{{ detection.backendLabel || detection.backend }}</dd>
            </div>
            <div>
              <dt>IPC 地址</dt>
              <dd v-if="detection.ipcSuccess">{{ detection.ipcTransport }} · {{ detection.ipcEndpoint }}</dd>
              <dd v-else class="warn-text">{{ detection.ipcError || '未探测到' }}</dd>
            </div>
            <div>
              <dt>远端域名</dt>
              <dd>
                {{ detection.remoteBaseUrl }}
                <span v-if="detection.remoteBaseUrlSource" class="muted-inline">来自 {{ detection.remoteBaseUrlSource }}</span>
              </dd>
            </div>
            <div>
              <dt>远端代理</dt>
              <dd :class="{ 'warn-text': detection.remoteProxyError }">{{ remoteProxyStatus }}</dd>
            </div>
            <div>
              <dt>登录态来源</dt>
              <dd v-if="detection.remoteCredentialSuccess">{{ detection.remoteCredentialSource }}</dd>
              <dd v-else class="warn-text">{{ detection.remoteCredentialError || '未探测到' }}</dd>
            </div>
            <div v-if="detection.remoteCredentialSuccess">
              <dt>账号 / 机器</dt>
              <dd>{{ detection.remoteUserId || '未知用户' }} · {{ detection.remoteMachineId || '未知机器' }}</dd>
            </div>
            <div v-if="detection.remoteCredentialSuccess">
              <dt>登录态有效期</dt>
              <dd :class="{ 'warn-text': detection.remoteTokenExpired }">
                {{ formattedTokenExpireAt || '未提供' }}
                <span v-if="detection.remoteTokenExpired">（已过期）</span>
              </dd>
            </div>
          </dl>
        </div>
      </div>

      <div class="glass-panel">
        <div class="panel-header">
          <div>
            <h2>模型与请求策略</h2>
            <p>直接调整并发和重试；保存后重启代理生效。</p>
          </div>
        </div>
        <div class="form-grid">
          <div class="field span-2">
            <label for="default-model">默认模型</label>
            <input id="default-model" v-model="config.Model" list="settings-models" type="text" placeholder="选择或输入模型 ID" />
            <datalist id="settings-models">
              <option v-for="model in models" :key="model.id" :value="model.id">{{ model.name }}</option>
            </datalist>
            <small>客户端未指定模型时使用。候选列表来自模型页的最近一次探测。</small>
          </div>
          <div class="field">
            <label for="max-concurrent">最大并发请求数</label>
            <input id="max-concurrent" v-model.number="config.MaxConcurrentRequests" type="number" min="1" max="16" step="1" />
            <small>默认 4，可设 1–16；超出数量的请求排队等待。</small>
          </div>
        </div>
        <fieldset class="settings-fieldset" :disabled="isIPCBackend">
          <div class="form-grid">
            <div class="field">
              <label for="retry-count">远程 429 重试次数</label>
              <input id="retry-count" v-model.number="config.RemoteRetryCount" type="number" min="0" max="10" step="1" />
              <small>仅远程模式；0 表示不重试，默认 3 次。</small>
            </div>
            <div class="field">
              <label for="retry-delay">初始退避时间（毫秒）</label>
              <input id="retry-delay" v-model.number="config.RemoteRetryBaseDelayMS" type="number" min="100" max="60000" step="100" />
              <small>默认 1000；后续指数递增并加入抖动，遵守上游等待指令。</small>
            </div>
          </div>
        </fieldset>
        <div class="field span-2 switch-field">
          <div>
            <label for="aggregate-tool-stream">工具调用流式兼容模式</label>
            <p>关闭时实时返回；开启时等整轮完成后集中返回，可能使客户端 TPS 偏高。</p>
          </div>
          <label class="switch">
            <input id="aggregate-tool-stream" v-model="config.AggregateToolStream" type="checkbox" />
            <span></span>
          </label>
        </div>
        <div class="hint-box compact-hint">
          <span>不会缓存失败或限制新请求频率。模型回退按“服务监听”中的开关及顺序执行。</span>
        </div>
      </div>

      <div class="glass-panel">
        <div class="panel-header">
          <div>
            <h2>会话与环境</h2>
            <p>仅在 IPC 插件模式下生效，影响 Lingma / QoderCN 会话上下文和工具执行环境。</p>
          </div>
          <span class="status-chip" :class="isIPCBackend ? 'ok' : 'warn'">{{ isIPCBackend ? '仅 IPC 生效' : '远端模式忽略' }}</span>
        </div>
        <div v-if="!isIPCBackend" class="hint-box compact-hint">
          <strong>当前为远端 API 模式</strong>
          <span>右侧这组参数不会参与远端请求，只在切换到 IPC 插件模式后生效。</span>
        </div>
        <fieldset class="settings-fieldset" :disabled="!isIPCBackend">
        <div class="form-grid compact-form-grid">
          <div class="field">
            <label>模式</label>
            <div class="custom-select" :class="{ open: openSelect === 'Mode' }">
              <button type="button" @click="toggleSelect('Mode')">
                <span>{{ selectLabel('Mode') }}</span>
                <i class="bi bi-chevron-down" aria-hidden="true"></i>
              </button>
              <div v-if="openSelect === 'Mode'" class="select-menu">
                <button
                  v-for="option in selectOptions.Mode"
                  :key="option.value"
                  :class="{ selected: option.value === config.Mode }"
                  type="button"
                  @click="chooseOption('Mode', option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
          </div>
          <div class="field">
            <label>Shell 类型</label>
            <div class="custom-select" :class="{ open: openSelect === 'ShellType' }">
              <button type="button" @click="toggleSelect('ShellType')">
                <span>{{ selectLabel('ShellType') }}</span>
                <i class="bi bi-chevron-down" aria-hidden="true"></i>
              </button>
              <div v-if="openSelect === 'ShellType'" class="select-menu">
                <button
                  v-for="option in selectOptions.ShellType"
                  :key="option.value"
                  :class="{ selected: option.value === config.ShellType }"
                  type="button"
                  @click="chooseOption('ShellType', option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
          </div>
          <div class="field">
            <label>会话策略</label>
            <div class="custom-select" :class="{ open: openSelect === 'SessionMode' }">
              <button type="button" @click="toggleSelect('SessionMode')">
                <span>{{ selectLabel('SessionMode') }}</span>
                <i class="bi bi-chevron-down" aria-hidden="true"></i>
              </button>
              <div v-if="openSelect === 'SessionMode'" class="select-menu">
                <button
                  v-for="option in selectOptions.SessionMode"
                  :key="option.value"
                  :class="{ selected: option.value === config.SessionMode }"
                  type="button"
                  @click="chooseOption('SessionMode', option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
          </div>
          <div class="field">
            <label>当前文件</label>
            <input v-model="config.CurrentFilePath" type="text" placeholder="可选" />
          </div>
          <div class="field span-2">
            <label>工作目录</label>
            <input v-model="config.Cwd" type="text" placeholder="Lingma / QoderCN 创建 session 时使用的 cwd" />
          </div>
        </div>
        </fieldset>
      </div>
    </section>
  </div>
</template>
