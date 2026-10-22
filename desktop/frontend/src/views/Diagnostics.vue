<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { RunDiagnostics, GetActivity } from '../../wailsjs/go/main/App.js'
const checks=ref([]), activity=ref([]), busy=ref(false), error=ref('')
let timer
const label=(value)=>({received:'已接收',queued:'排队',upstream:'等待上游',generating:'生成中',retry_wait:'等待重试',fallback:'切换模型',completed:'结束'})[value] || value
async function run(generate=false) {
 busy.value=true; error.value=''
 try { checks.value=await RunDiagnostics(generate) } catch(e) {error.value=String(e)}
 finally {busy.value=false}
}
async function poll() { try {activity.value=await GetActivity()} catch {} }
onMounted(()=>{poll();timer=setInterval(poll,1500)})
onUnmounted(()=>clearInterval(timer))
</script>
<template>
 <div class="page">
  <div class="page-title"><div><h1>诊断</h1><p>连接检查不生成内容；最小生成测试会使用当前账号额度。</p></div>
   <div class="toolbar"><button class="primary-button" :disabled="busy" @click="run(false)">{{busy?'检查中…':'检查连接与目录'}}</button><button class="secondary-button" :disabled="busy" @click="run(true)">最小生成测试（使用额度）</button></div>
  </div>
  <p v-if="error" role="alert">{{error}}</p>
  <section class="glass-panel"><h2>检查结果</h2><p v-if="!checks.length">点击检查，确认登录、目录和默认模型状态。</p>
   <div v-for="(check,i) in checks" :key="i" class="detail-section"><strong>{{({ok:'通过',warning:'注意',error:'失败'})[check.status]}} · {{check.message}}</strong><p>{{check.action}}</p></div>
  </section>
  <section class="glass-panel"><h2>进行中的请求</h2><p v-if="!activity.length">当前没有进行中的请求。</p>
   <div v-for="item in activity" :key="item.id" class="detail-section"><strong>{{label(item.stage)}} · {{item.actualModel || item.requestedModel || '模型待确定'}}</strong><p>{{item.path}} · {{item.elapsedMs}} ms</p><small>{{item.id}}</small></div>
  </section>
 </div>
</template>
