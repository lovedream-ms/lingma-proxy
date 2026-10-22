<script setup>
import { ref, watch } from 'vue'
import { PreviewClientConfig, ApplyClientConfig, RestoreClientConfig } from '../../wailsjs/go/main/App.js'
import { ClipboardSetText } from '../../wailsjs/runtime'
const tool=ref('claude'), preview=ref(null), busy=ref(false), notice=ref(''), backup=ref('')
watch(tool,()=>{preview.value=null;notice.value='';backup.value=localStorage.getItem('lingma-backup-'+tool.value)||''})
backup.value=localStorage.getItem('lingma-backup-'+tool.value)||''
async function load() {busy.value=true;notice.value='';try{preview.value=await PreviewClientConfig(tool.value)}catch(e){notice.value=String(e);preview.value=null}finally{busy.value=false}}
async function apply() {if(!preview.value)return;busy.value=true;try{backup.value=await ApplyClientConfig(tool.value,preview.value.fingerprint);localStorage.setItem('lingma-backup-'+tool.value,backup.value);notice.value='已应用并备份原文件。'+preview.value.instruction;preview.value=null}catch(e){notice.value=String(e)}finally{busy.value=false}}
async function restore(){busy.value=true;try{await RestoreClientConfig(tool.value,backup.value);notice.value='已恢复原配置';preview.value=null}catch(e){notice.value=String(e)}finally{busy.value=false}}
async function copy(){try{await ClipboardSetText(preview.value.after);notice.value='已复制配置（包含代理访问密钥）'}catch(e){notice.value=String(e)}}
</script>
<template>
 <div class="page">
  <div class="page-title"><div><h1>客户端接入</h1><p>使用已保存的代理地址、默认模型和访问密钥。先预览，再应用；不会自动改动客户端。</p></div></div>
  <section class="glass-panel">
   <div class="form-grid"><div class="field"><label>客户端</label><select v-model="tool"><option value="claude">Claude Code</option><option value="codex">Codex CLI</option><option value="continue">Continue</option></select></div></div>
   <button class="primary-button" :disabled="busy" @click="load">{{busy?'处理中…':'预览配置修改'}}</button>
   <p role="status">{{notice}}</p>
   <template v-if="preview"><p>文件：{{preview.path}}</p><p>{{preview.instruction}}</p><p>下方配置可能包含密钥。应用前会保存原文件；其他程序修改了文件时，请重新预览。</p>
    <div class="grid-2"><div><h3>当前内容</h3><pre class="client-preview">{{preview.before || '文件尚不存在'}}</pre></div><div><h3>应用后内容</h3><pre class="client-preview">{{preview.after}}</pre></div></div>
    <div class="toolbar"><button class="primary-button" :disabled="busy" @click="apply">备份并应用此修改</button><button class="secondary-button" :disabled="busy" @click="copy">复制应用后配置</button></div>
   </template>
  </section>
  <section class="glass-panel"><h2>恢复原配置</h2><p>备份保存在客户端配置目录的 .lingma-proxy-backups 中。恢复时会检查是否存在后续修改。</p><div class="field"><label>备份 ID</label><input v-model="backup" placeholder="应用成功后自动填入" /></div><button class="secondary-button" :disabled="busy || !backup" @click="restore">恢复此备份</button></section>
 </div>
</template>
<style scoped>.client-preview{max-height:360px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;font-size:12px;padding:16px;background:var(--panel-bg,rgba(128,128,128,.08));border-radius:12px}</style>
