<script setup>
import { computed, onMounted, ref } from 'vue'
import { GetModels, GetStatus, RefreshModels } from '../../wailsjs/go/main/App.js'
import { ClipboardSetText } from '../../wailsjs/runtime'
import { modelIcon } from '../modelIcons'

const emit = defineEmits(['log', 'status', 'notice'])

const models = ref([])
const status = ref({ running: false, addr: '', models: 0 })
const loading = ref(false)
const query = ref('')

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return models.value
  return models.value.filter((model) => `${model.id} ${model.name}`.toLowerCase().includes(q))
})

function modelSpec(model) {
  const format = (value) => value >= 1000000 ? `${value / 1000000}M` : `${value / 1000}K`
  const maximum = model.max_context_length || model.context_window
  const modalities = model.input_modalities || []
  const vision = model.supports_vision ?? (modalities.length ? modalities.includes('image') : null)
  return {
    context: maximum ? `最大 ${format(maximum)}` : '上下文未公开',
    capability: vision === null ? '图像能力未公开' : (vision ? '文本 / 图像' : '文本'),
    source: model.context_window ? `默认 ${format(model.context_window)}` : '账号返回',
  }
}

async function loadCachedModels() {
  loading.value = true
  try {
    status.value = await GetStatus()
    models.value = await GetModels()
  } catch (e) {
    emit('log', 'error', '模型缓存读取失败：' + (e.message || String(e)))
  } finally {
    loading.value = false
  }
}

async function refresh() {
  loading.value = true
  try {
    status.value = await GetStatus()
    models.value = await RefreshModels()
    emit('log', 'info', `模型列表刷新完成：${models.value.length} 个`)
  } catch (e) {
    emit('log', 'error', '模型列表刷新失败：' + (e.message || String(e)) + '。自动探测失败时请到设置页手动填写 WebSocket、macOS Socket 或 Windows Named Pipe。')
  } finally {
    loading.value = false
  }
}

async function copyModelName(model) {
  if (!model?.id) return
  try {
    await ClipboardSetText(model.id)
    emit('notice', `已复制模型 ID：${model.id}`)
  } catch (e) {
    try {
      await navigator.clipboard?.writeText(model.id)
      emit('notice', `已复制模型 ID：${model.id}`)
    } catch (fallbackError) {
      emit('log', 'error', '模型 ID 复制失败：' + (fallbackError.message || String(fallbackError)))
    }
  }
}

onMounted(loadCachedModels)
</script>

<template>
  <div class="page">
    <div class="page-title">
      <div>
        <h1>模型</h1>
        <p>来自 Lingma / QoderCN 的可用模型列表，第三方客户端可以直接使用这些 ID。</p>
      </div>
      <button class="primary-button" type="button" :disabled="loading" @click="refresh">
        {{ loading ? '刷新中...' : '刷新模型' }}
      </button>
    </div>

    <section class="grid-3">
      <div class="metric">
        <label>代理状态</label>
        <strong>{{ status.running ? '运行中' : '未运行' }}</strong>
      </div>
      <div class="metric">
        <label>接口地址</label>
        <strong>{{ status.addr || '未启动' }}</strong>
      </div>
      <div class="metric">
        <label>模型数量</label>
        <strong>{{ models.length }}</strong>
      </div>
    </section>

    <section class="glass-panel">
      <div class="panel-header">
        <div>
          <h2>可用模型</h2>
          <p>模型目录、代理适配路径与当前环境可用性分别显示。标记不代表此模型已通过生成测试；上下文可能包含兼容提示。</p>
        </div>
        <input v-model="query" class="search-input" type="search" placeholder="搜索模型" style="max-width: 260px" />
      </div>

      <div v-if="filtered.length > 0" class="models-list model-page-list hidden-scrollbar">
        <button
          v-for="model in filtered"
          :key="model.id"
          class="model-row model-list-row model-choice"
          type="button"
          :title="`复制模型 ID：${model.id}`"
          @click="copyModelName(model)"
        >
          <span
            class="model-brand-icon"
            :style="{ '--model-icon': `url(${modelIcon(model).src})`, '--model-icon-color': modelIcon(model).color }"
            aria-hidden="true"
          ></span>
          <div>
            <div class="model-name">{{ model.name || model.id }}</div>
            <div class="model-meta">{{ model.id }}</div>
          </div>
          <div class="model-specs">
            <span class="spec-chip strong">{{ modelSpec(model).context }}</span>
            <span class="spec-chip">{{ modelSpec(model).capability }}</span>
            <span class="spec-chip muted-chip">{{ modelSpec(model).source }}</span>
 <span class="spec-chip" :title="model.adapter_capabilities?.tools?.detail">工具：提示适配</span>
 <span class="spec-chip" :title="model.adapter_capabilities?.images?.detail">图像：需要 IPC</span>
 <span class="spec-chip" :title="model.adapter_capabilities?.structured_output?.detail">严格 JSON Schema：不支持</span>
          </div>
        </button>
      </div>
      <div v-else class="empty-state">启动代理并刷新后会显示模型。</div>
    </section>
  </div>
</template>
