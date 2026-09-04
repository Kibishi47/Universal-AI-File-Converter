<template>
  <main class="min-h-screen bg-dark-950 text-slate-100 flex flex-col justify-between selection:bg-brand-500/30 selection:text-brand-300">
    <!-- Ambient glowing backgrounds -->
    <div class="fixed top-0 left-1/2 -translate-x-1/2 w-full max-w-7xl h-96 bg-brand-500/10 blur-[120px] pointer-events-none -z-10"></div>
    <div class="fixed bottom-0 right-0 w-96 h-96 bg-emerald-500/5 blur-[120px] pointer-events-none -z-10"></div>

    <!-- Navigation Bar -->
    <header class="border-b border-slate-800/60 bg-dark-950/80 backdrop-blur-md sticky top-0 z-50">
      <div class="max-w-5xl mx-auto px-4 sm:px-6 h-16 flex items-center justify-between">
        <div class="flex items-center gap-3">
          <div class="w-9 h-9 rounded-xl bg-gradient-to-tr from-brand-600 to-emerald-400 flex items-center justify-center text-dark-950 shadow-md shadow-brand-500/20 font-black">
            ⚡
          </div>
          <span class="text-base font-extrabold tracking-tight text-white">
            Convert<span class="text-brand-400">AI</span>
          </span>
        </div>

        <div class="flex items-center gap-3">
          <span class="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold bg-brand-500/10 text-brand-400 border border-brand-500/20">
            <span class="w-2 h-2 rounded-full bg-brand-400 animate-pulse"></span>
            LLM Local Actif
          </span>
        </div>
      </div>
    </header>

    <!-- Main Content Area -->
    <section class="max-w-5xl mx-auto px-4 sm:px-6 py-10 w-full flex-1 space-y-8">
      <!-- Hero Header -->
      <div class="text-center space-y-3 max-w-2xl mx-auto">
        <h1 class="text-3xl sm:text-4xl font-extrabold tracking-tight text-white">
          Convertisseur Universel <span class="bg-clip-text text-transparent bg-gradient-to-r from-brand-400 to-emerald-400">Propulsé par IA</span>
        </h1>
        <p class="text-sm sm:text-base text-slate-400 leading-relaxed">
          Transformez n'importe quel fichier sans limites ni inscription. L'IA analyse la structure réelle et orchestre la meilleure suite d'outils CLI en temps réel.
        </p>
      </div>

      <!-- File Dropzone -->
      <FileDropzone @files-selected="addFiles" />

      <!-- Global Controls (Show if files present) -->
      <GlobalControls
        v-if="files.length > 0"
        :model-value="globalTargetExt"
        :has-files="files.length > 0"
        :has-pending-files="files.some(f => f.status === 'pending')"
        :has-completed-files="hasCompletedFiles"
        :is-processing="isUploading"
        @update:model-value="setGlobalTarget"
        @convert-all="convertAll"
        @clear="clearAll"
        @download-zip="downloadAllZip"
      />

      <!-- Files Queue List -->
      <ConversionQueue
        v-if="files.length > 0"
        :files="files"
        @convert="convertFile"
        @remove="removeFile"
      />

      <!-- Privacy Notice -->
      <PrivacyNotice />
    </section>

    <!-- Footer -->
    <footer class="border-t border-slate-800/60 py-6 text-center text-xs text-slate-500">
      <div class="max-w-5xl mx-auto px-4 flex flex-col sm:flex-row items-center justify-between gap-2">
        <p>Orchestration IA locale · Décodage contraint · Zéro journalisation</p>
        <p class="font-mono text-[11px] text-slate-600">Session : {{ sessionUUID }}</p>
      </div>
    </footer>
  </main>
</template>

<script setup lang="ts">
const { sessionUUID } = useSession()
const {
  files,
  globalTargetExt,
  isUploading,
  hasCompletedFiles,
  setGlobalTarget,
  addFiles,
  removeFile,
  clearAll,
  convertFile,
  convertAll,
  downloadAllZip,
} = useConverter()
</script>
