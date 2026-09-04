<template>
  <div
    class="relative rounded-2xl border-2 border-dashed transition-all duration-300 p-8 sm:p-12 text-center cursor-pointer group"
    :class="[
      isDragging
        ? 'border-brand-500 bg-brand-500/10 scale-[1.01]'
        : 'border-slate-700/80 hover:border-slate-500 bg-dark-900/60 hover:bg-dark-900/90'
    ]"
    @dragover.prevent="isDragging = true"
    @dragleave.prevent="isDragging = false"
    @drop.prevent="handleDrop"
    @click="triggerFileInput"
  >
    <input
      ref="fileInput"
      type="file"
      multiple
      class="hidden"
      @change="handleFileChange"
    />

    <div class="flex flex-col items-center justify-center space-y-4">
      <div
        class="w-16 h-16 rounded-2xl bg-gradient-to-br from-brand-500/20 to-emerald-500/10 border border-brand-500/30 flex items-center justify-center text-brand-400 group-hover:scale-110 transition-transform duration-300 shadow-lg shadow-brand-500/5"
      >
        <svg class="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
        </svg>
      </div>

      <div class="space-y-1">
        <h3 class="text-xl font-bold text-white tracking-tight">
          Glissez-déposez vos fichiers ici
        </h3>
        <p class="text-sm text-slate-400 max-w-sm mx-auto">
          Prend en charge tous types de fichiers (documents, vidéos, images, audios, archives).
        </p>
      </div>

      <div class="pt-2">
        <button
          type="button"
          class="px-6 py-2.5 rounded-xl bg-gradient-to-r from-brand-600 to-emerald-600 hover:from-brand-500 hover:to-emerald-500 text-white font-semibold text-sm shadow-md hover:shadow-brand-500/20 transition-all duration-200"
        >
          Sélectionner des fichiers
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const emit = defineEmits<{
  (e: 'filesSelected', files: FileList | File[]): void
}>()

const isDragging = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const triggerFileInput = () => {
  fileInput.value?.click()
}

const handleFileChange = (e: Event) => {
  const target = e.target as HTMLInputElement
  if (target.files && target.files.length > 0) {
    emit('filesSelected', target.files)
    target.value = '' // Reset input
  }
}

const handleDrop = (e: DragEvent) => {
  isDragging.value = false
  if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
    emit('filesSelected', e.dataTransfer.files)
  }
}
</script>
