# ⚡ Universal AI File Converter

Plateforme universelle de conversion de fichiers éphémère, résiliente, respectueuse de la vie privée (zéro tracking, aucune donnée personnelle stockée) et orchestrée par un LLM local via décodage contraint (`llama.cpp`).

---

## 🏗️ Architecture du Monorepo

```text
├── .github/
│   └── workflows/ci.yml         # Validation continue Go & Nuxt
├── docker/
│   ├── dev/
│   │   ├── docker-compose.yml   # Stack complète locale
│   │   └── Dockerfile.worker    # Worker dev avec suite CLI + pkg manager
│   └── prod/
│       ├── Dockerfile.api       # Multi-stage minimal pour l'API Go
│       ├── Dockerfile.worker    # Image de conversion enrichie pour la prod
│       └── Dockerfile.frontend  # Image de production Nuxt 3 (SSR/Nitro)
├── apps/
│   ├── web/                     # Frontend Nuxt 3 (Tailwind CSS, SSE, zéro cookie)
│   │   ├── assets/css/main.css
│   │   ├── components/          # Dropzone, Queue, Controls, Privacy
│   │   ├── composables/         # useSession (UUID v4 éphémère), useConverter
│   │   └── nuxt.config.ts
│   └── api/                     # Backend & Worker en Go
│       ├── cmd/
│       │   ├── server/          # API Chi HTTP, Upload, SSE stream, Zip generator
│       │   └── worker/          # Worker asynchrone Asynq
│       ├── internal/
│       │   ├── config/          # Gestionnaire de variables d'environnement
│       │   ├── detector/        # Magic bytes (filetype + MIME fallback)
│       │   ├── storage/         # Client S3 compatible SeaweedFS (TTL 2h)
│       │   ├── queue/           # File Asynq & Redis Pub/Sub temps réel
│       │   ├── llm/             # Client llama.cpp + schema JSON contraint & fallback
│       │   ├── github/          # Déduplication et création d'issues GitHub
│       │   ├── runner/          # Exécuteur sandboxé (timeout 120s) & installation dynamique
│       │   └── zipper/          # Stream d'archives ZIP à la volée
│       ├── go.mod
│       └── go.sum
├── scripts/
│   └── init-seaweedfs.sh        # Initialisation du bucket S3
└── README.md
```

---

## 🚀 Démarrage rapide en local (Docker Compose)

### 1. Pré-requis
- Docker et Docker Compose installés.
- *(Optionnel pour le LLM)* Déposez un modèle GGUF (ex: `Llama-3-8B-Instruct-Q4_K_M.gguf`) dans `docker/dev/models/default.gguf`. Si aucun modèle n'est monté, le worker bascule automatiquement sur le moteur de règles déterministe haute fiabilité intégré.

### 2. Démarrer toute la stack

Avec le **Makefile** (recommandé) :
```bash
# Démarrage avec build
make dev-build

# Ou simple démarrage
make dev
```

Ou directement avec Docker Compose :
```bash
docker compose -f docker/dev/docker-compose.yml up --build
```

### 3. Commandes utiles (Makefile)
- `make dev` / `make dev-build` : Lancer la stack locale
- `make down` : Stopper tous les conteneurs
- `make logs` / `make logs-api` / `make logs-worker` / `make logs-web` : Suivre les logs en direct
- `make ps` : Afficher l'état des conteneurs
- `make build` : Compiler les binaires Go et le bundle Nuxt en local
- `make clean` : Nettoyer conteneurs, volumes et dossiers de build
- `make init-bucket` : Initialiser le bucket S3 sur SeaweedFS

### 3. Services disponibles
| Service | URL / Port | Description |
| :--- | :--- | :--- |
| **Frontend Web** | `http://localhost:3000` | Interface utilisateur Nuxt 3 |
| **API Backend** | `http://localhost:8080` | Chi Router, SSE & Endpoints S3 |
| **SeaweedFS S3** | `http://localhost:8333` | Endpoint stockage compatible S3 |
| **Redis** | `localhost:6379` | Queue Asynq & broker Pub/Sub |
| **Llama Engine** | `http://localhost:8081` | Moteur `llama.cpp:server` |

---

## 🔒 Confidentialité & Sécurité
- **Zéro tracking :** Aucun cookie d'authentification, aucun tracker publicitaire, aucun analytics.
- **Session volatile :** Un identifiant `UUID v4` éphémère est généré côté client en session mémoire.
- **Rétention éphémère :** Tous les fichiers déposés et convertis sur SeaweedFS ont un tag d'expiration TTL programmé à **2 heures**, après quoi ils sont purgés définitivement.
- **Isolation :** Exécutions CLI isolées avec timeout strict de 120 secondes par conversion.

---

## 🌐 Guide de déploiement sur VPS OVH avec Coolify

Coolify permet de gérer l'orchestration Docker facilement sur un VPS Debian/Ubuntu.

### Étape 1 : Provisionner les dépendances One-Click
1. Connectez-vous à votre interface **Coolify**.
2. Dans votre projet / environnement, cliquez sur **+ New Resource** > **Service** :
   - Ajoutez un service **Redis** (One-click). Notez le nom du host interne (ex: `redis` ou `redis-xxxx`).
   - Ajoutez un service **SeaweedFS** ou **MinIO** (S3-compatible).
     - Définissez le bucket `conversions`.
     - Récupérez l'endpoint interne S3, l'Access Key et le Secret Key.

### Étape 2 : Déployer le conteneur LLM (`llama.cpp:server`)
1. Ajoutez une ressource de type **Docker Image** :
   - Image : `ghcr.io/ggerganov/llama.cpp:server`
2. **Configuration du volume hôte :**
   - Créez un dossier sur votre VPS (ex: `/var/data/models`) et téléchargez-y votre modèle GGUF :
     ```bash
     mkdir -p /var/data/models
     curl -L -o /var/data/models/default.gguf "https://huggingface.co/bartowski/Meta-Llama-3-8B-Instruct-GGUF/resolve/main/Meta-Llama-3-8B-Instruct-Q4_K_M.gguf"
     ```
   - Dans Coolify, configurez un bind mount de stockage :
     - Source : `/var/data/models`
     - Destination : `/models`
3. **Commande d'exécution :**
   ```text
   -m /models/default.gguf --host 0.0.0.0 --port 8080 -c 2048 --n-gpu-layers 0
   ```
4. Exposez le port 8080 sur le réseau interne de Coolify (ex: `http://llama-engine:8080`).

### Étape 3 : Déployer l'API Go
1. Ajoutez une ressource depuis votre dépôt Git (Repository).
2. Spécifiez :
   - **Base Directory :** `/`
   - **Dockerfile Path :** `docker/prod/Dockerfile.api`
3. Renseignez les variables d'environnement dans Coolify :
   ```env
   PORT=8080
   REDIS_ADDR=redis:6379
   S3_ENDPOINT=seaweedfs:8333
   S3_BUCKET=conversions
   S3_ACCESS_KEY=votre_access_key
   S3_SECRET_KEY=votre_secret_key
   S3_USE_SSL=false
   LLM_BASE_URL=http://llama-engine:8080
   LLM_MODEL=default
   GITHUB_TOKEN=ghp_xxxxxxxxxxxx
   GITHUB_OWNER=votre_organisation_ou_compte
   GITHUB_REPO=file-convertor
   MAX_UPLOAD_SIZE_MB=200
   ```
4. Configurez le domaine public pour l'API (ex: `api.converter.votredomaine.com`).

### Étape 4 : Déployer le Worker Go
1. Ajoutez une ressource depuis le même dépôt Git.
2. Spécifiez :
   - **Base Directory :** `/`
   - **Dockerfile Path :** `docker/prod/Dockerfile.worker`
3. Renseignez les mêmes variables d'environnement (`REDIS_ADDR`, `S3_*`, `LLM_*`, `GITHUB_*`).
4. Le worker ne nécessite aucun domaine exposé publiquement (il consomme uniquement Asynq/Redis).

### Étape 5 : Déployer le Frontend Nuxt 3
1. Ajoutez une ressource depuis le dépôt Git.
2. Spécifiez :
   - **Base Directory :** `/`
   - **Dockerfile Path :** `docker/prod/Dockerfile.frontend`
3. Renseignez la variable d'environnement publique pour pointer vers l'API :
   ```env
   NUXT_PUBLIC_API_BASE=https://api.converter.votredomaine.com
   PORT=3000
   HOST=0.0.0.0
   ```
4. Attribuez votre domaine principal (ex: `converter.votredomaine.com`). Coolify générera automatiquement les certificats SSL Let's Encrypt.
