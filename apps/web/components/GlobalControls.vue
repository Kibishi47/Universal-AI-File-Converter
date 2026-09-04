<template>
  <div class="glass-panel rounded-2xl p-4 sm:p-6 flex flex-col sm:flex-row items-center justify-between gap-4">
    <div class="flex items-center gap-3 w-full sm:w-auto">
      <span class="text-sm font-medium text-slate-300 whitespace-nowrap">
        Tout convertir en :
      </span>
      <select
        :value="modelValue"
        class="bg-dark-900 text-white text-sm font-semibold rounded-xl px-4 py-2.5 border border-slate-700 focus:outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500 transition-colors cursor-pointer"
        @change="$emit('update:modelValue', ($event.target as HTMLSelectElement).value)"
      >
        <optgroup label="Documents">
          <option value="pdf">PDF (.pdf)</option>
          <option value="docx">Word (.docx)</option>
          <option value="txt">Texte brut (.txt)</option>
          <option value="html">HTML (.html)</option>
          <option value="md">Markdown (.md)</option>
        </optgroup>
        <optgroup label="Images">
          <option value="png">PNG (.png)</option>
          <option value="webp">WebP (.webp)</option>
          <option value="jpg">JPEG (.jpg)</option>
          <option value="svg">SVG (.svg)</option>
        </optgroup>
        <optgroup label="Audio / Vidéo">
          <option value="mp4">MP4 (.mp4)</option>
          <option value="mp3">MP3 (.mp3)</option>
          <option value="wav">WAV (.wav)</option>
          <option value="webm">WebM (.webm)</option>
        </optgroup>
        <optgroup label="Données & Tableurs">
          <option value="csv">CSV (.csv)</option>
          <option value="xlsx">Excel (.xlsx)</option>
        </optgroup>
      </select>
    </div>

    <div class="flex items-center gap-3 w-full sm:w-auto justify-end">
      <button
        v-if="hasFiles"
        type="button"
        class="px-4 py-2.5 rounded-xl border border-slate-700 hover:border-slate-500 hover:bg-slate-800/40 text-slate-300 text-sm font-medium transition-colors"
        @click="$emit('clear')"
      >
        Vider la liste
      </button>

      <button
        v-if="hasCompletedFiles"
        type="button"
        class="px-5 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-white font-semibold text-sm border border-slate-600 flex items-center gap-2 shadow-sm transition-colors"
        @click="$emit('downloadZip')"
      >
        <svg class="w-4 h-4 text-brand-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
        </svg>
        Tout télécharger (.ZIP)
      </button>

      <button
        v-if="hasPendingFiles"
        type="button"
        :disabled="isProcessing"
        class="px-6 py-2.5 rounded-xl bg-gradient-to-r from-brand-600 to-emerald-600 hover:from-brand-500 hover:to-emerald-500 disabled:opacity-50 text-white font-bold text-sm shadow-md shadow-brand-500/20 transition-all flex items-center gap-2"
        @click="$emit('convertAll')"
      >
        <span v-if="isProcessing" class="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
        <span>Convertir tous les fichiers</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  modelValue: string
  hasFiles: boolean
  hasPendingFiles: boolean
  hasCompletedFiles: boolean
  isProcessing: boolean
}>()

defineEmits<{
  (e: 'update:modelValue', val: string): void
  (e: 'convertAll'): void
  (e: 'clear'): void
  (e: 'downloadZip'): void
}>()
</script>
