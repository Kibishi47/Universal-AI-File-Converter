<template>
  <div class="space-y-3">
    <div
      v-for="item in files"
      :key="item.id"
      class="glass-card rounded-2xl p-4 sm:p-5 transition-all duration-200 hover:border-slate-600/80 group"
    >
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <!-- File Info -->
        <div class="flex items-center gap-3.5 min-w-0">
          <div class="w-10 h-10 rounded-xl bg-slate-800/80 border border-slate-700/60 flex items-center justify-center text-slate-300 font-bold uppercase text-xs flex-shrink-0">
            {{ item.sourceExt || '?' }}
          </div>
          <div class="min-w-0">
            <h4 class="text-sm font-semibold text-white truncate max-w-xs sm:max-w-md" :title="item.name">
              {{ item.name }}
            </h4>
            <p class="text-xs text-slate-400">
              {{ formatBytes(item.size) }}
            </p>
          </div>
        </div>

        <!-- Conversion Target & Status Actions -->
        <div class="flex items-center gap-3 flex-wrap sm:flex-nowrap justify-between sm:justify-end">
          <div class="flex items-center gap-2">
            <span class="text-xs text-slate-400 font-medium">Vers</span>
            <select
              v-if="item.status === 'pending'"
              v-model="item.targetExt"
              class="bg-dark-900 text-white text-xs font-semibold rounded-lg px-3 py-1.5 border border-slate-700 focus:outline-none focus:border-brand-500 cursor-pointer"
            >
              <option value="pdf">PDF</option>
              <option value="docx">DOCX</option>
              <option value="txt">TXT</option>
              <option value="html">HTML</option>
              <option value="md">MD</option>
              <option value="png">PNG</option>
              <option value="webp">WEBP</option>
              <option value="jpg">JPG</option>
              <option value="mp4">MP4</option>
              <option value="mp3">MP3</option>
              <option value="csv">CSV</option>
            </select>
            <span
              v-else
              class="px-2.5 py-1 rounded-md bg-dark-900 border border-slate-700 text-brand-400 font-mono text-xs font-bold uppercase"
            >
              .{{ item.targetExt }}
            </span>
          </div>

          <!-- Status Badges & Actions -->
          <div class="flex items-center gap-2">
            <!-- Pending State -->
            <button
              v-if="item.status === 'pending'"
              type="button"
              class="px-3.5 py-1.5 rounded-lg bg-brand-600/20 hover:bg-brand-600/30 text-brand-400 border border-brand-500/30 text-xs font-semibold transition-colors"
              @click="$emit('convert', item)"
            >
              Convertir
            </button>

            <!-- Converting / Analyzing State -->
            <div
              v-else-if="['queued', 'analyzing', 'installing_tool', 'converting'].includes(item.status)"
              class="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs font-medium"
            >
              <span class="w-3 h-3 border-2 border-amber-400/30 border-t-amber-400 rounded-full animate-spin"></span>
              <span>{{ statusLabel(item.status) }}</span>
            </div>

            <!-- Ready State -->
            <a
              v-else-if="item.status === 'ready' && item.downloadUrl"
              :href="item.downloadUrl"
              download
              class="px-3.5 py-1.5 rounded-lg bg-brand-600 hover:bg-brand-500 text-white text-xs font-bold flex items-center gap-1.5 shadow-sm transition-colors"
            >
              <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
              </svg>
              Télécharger
            </a>

            <!-- Error State -->
            <div
              v-else-if="item.status === 'error'"
              class="px-3 py-1.5 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-xs font-medium"
              :title="item.error || 'Erreur inconnue'"
            >
              Échec
            </div>

            <!-- Remove Button -->
            <button
              type="button"
              class="p-1.5 rounded-lg text-slate-500 hover:text-red-400 hover:bg-red-500/10 transition-colors"
              title="Retirer"
              @click="$emit('remove', item.id)"
            >
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>
      </div>

      <!-- Live Progress Bar & Info Message -->
      <div v-if="['queued', 'analyzing', 'installing_tool', 'converting'].includes(item.status)" class="mt-3 space-y-1.5">
        <div class="flex justify-between items-center text-[11px] text-slate-400">
          <span>{{ item.message }}</span>
          <span>{{ item.progress }}%</span>
        </div>
        <div class="w-full h-1.5 bg-slate-800 rounded-full overflow-hidden">
          <div
            class="h-full bg-gradient-to-r from-brand-500 to-emerald-400 rounded-full transition-all duration-300"
            :style="{ width: `${item.progress}%` }"
          ></div>
        </div>
      </div>

      <!-- Error message display -->
      <div v-if="item.status === 'error' && item.error" class="mt-2 text-xs text-red-400/90 bg-red-950/40 p-2.5 rounded-lg border border-red-800/30">
        {{ item.error }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { ConversionFile } from '~/composables/useConverter'

defineProps<{
  files: ConversionFile[]
}>()

defineEmits<{
  (e: 'convert', item: ConversionFile): void
  (e: 'remove', id: string): void
}>()

const statusLabel = (status: string) => {
  switch (status) {
    case 'queued': return 'En file'
    case 'analyzing': return 'Analyse IA'
    case 'installing_tool': return 'Préparation outil'
    case 'converting': return 'Conversion...'
    default: return 'Traitement'
  }
}

const formatBytes = (bytes: number, decimals = 1) => {
  if (bytes === 0) return '0 Octets'
  const k = 1024
  const dm = decimals < 0 ? 0 : decimals
  const sizes = ['Octets', 'Ko', 'Mo', 'Go']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i]
}
</script>
