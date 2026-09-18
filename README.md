# 📚 Wiki-rog

Ein **lokales RAG-System** (Retrieval-Augmented Generation), das ausschließlich
das Wissen aus einem **BookStack-Wiki** nutzt. Alles läuft lokal über
[Ollama](https://ollama.com) – es verlassen keine Daten den Server.

- **Datenquelle:** BookStack REST-API (nur diese Inhalte werden „gelernt")
- **Embeddings & LLM:** lokal via Ollama
- **Vektorspeicher:** ChromaDB (persistent auf der Platte)
- **Antworten:** strikt nur aus dem abgerufenen Wiki-Kontext – sonst
  „Das steht nicht im Wiki."

```
BookStack ──REST──> Ingest ──Chunks──> Embeddings (Ollama) ──> ChromaDB
                                                                   │
Frage ──> Embedding ──> Ähnlichkeitssuche ──> Kontext ──> LLM (Ollama) ──> Antwort + Quellen
```

## Warum „lernt" es nur BookStack-Daten?

Das Modell selbst wird **nicht** trainiert. Stattdessen wird bei jeder Frage
relevanter Text aus dem Wiki gesucht und dem LLM als Kontext mitgegeben. Der
System-Prompt zwingt das Modell, **nur** aus diesem Kontext zu antworten und
kein Weltwissen zu verwenden. So bleibt die Wissensbasis exakt auf BookStack
beschränkt und ist jederzeit aktuell (einfach neu indexieren).

## Voraussetzungen

1. **Python 3.10+**
2. **Ollama** installiert und gestartet (`ollama serve`), mit den Modellen:
   ```bash
   ollama pull llama3.2:1b       # kleines, CPU-taugliches Antwort-Modell
   ollama pull nomic-embed-text  # Embedding-Modell
   ```
3. Eine erreichbare **BookStack-Instanz** mit API-Token
   (BookStack → *Profil bearbeiten* → *API-Tokens*).

## Installation

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env      # dann .env ausfüllen (BookStack-URL + Token)
```

## Nutzung

```bash
# 1) Verbindungen prüfen (BookStack + Ollama)
python cli.py test

# 2) Wiki indexieren (beim ersten Mal; danach jederzeit erneut zum Aktualisieren)
python cli.py ingest            # inkrementell (aktualisiert geänderte Seiten)
python cli.py ingest --reset    # kompletter Neuaufbau

# 3) Fragen stellen
python cli.py ask "Wie beantrage ich Urlaub?"
python cli.py chat              # interaktiver Chat im Terminal

# 4) Weboberfläche
streamlit run app.py
```

## Konfiguration (`.env`)

| Variable | Bedeutung | Default |
|---|---|---|
| `BOOKSTACK_URL` | Basis-URL der BookStack-Instanz | – |
| `BOOKSTACK_TOKEN_ID` / `BOOKSTACK_TOKEN_SECRET` | API-Token | – |
| `BOOKSTACK_BOOK_IDS` | Nur diese Bücher indexieren (kommagetrennt) | alle |
| `BOOKSTACK_SHELF_IDS` | Nur Bücher dieser Regale indexieren | alle |
| `OLLAMA_URL` | Ollama-Endpoint | `http://localhost:11434` |
| `OLLAMA_LLM_MODEL` | Antwort-Modell | `llama3.1` |
| `OLLAMA_EMBED_MODEL` | Embedding-Modell | `nomic-embed-text` |
| `CHROMA_DIR` | Speicherort des Vektorindex | `./data/chroma` |
| `CHUNK_SIZE` / `CHUNK_OVERLAP` | Chunk-Größe/Überlappung (Zeichen) | `1000` / `150` |
| `RETRIEVAL_TOP_K` | Anzahl Kontext-Chunks je Frage | `5` |
| `RETRIEVAL_MAX_DISTANCE` | max. Cosine-Distanz für Treffer | `1.0` |

## Deployment auf Kubernetes

Der komplette Stack (Ollama mit kleinem CPU-Modell, Web-UI, Ingest) läuft im
Cluster – **ohne GPU**. Manifeste liegen unter `k8s/`.

**1) Image bauen und dem Cluster verfügbar machen**
```bash
docker build -t wiki-rog:latest .

# Beispiel minikube:      minikube image load wiki-rog:latest
# Beispiel kind:          kind load docker-image wiki-rog:latest
# Registry (Produktion):  docker tag wiki-rog:latest <registry>/wiki-rog:latest
#                         docker push <registry>/wiki-rog:latest
#     (dann image: in k8s/app.yaml & k8s/ingest-job.yaml anpassen)
```

**2) Konfiguration & Secret setzen**
```bash
# BOOKSTACK_URL usw. in k8s/configmap.yaml anpassen
cp k8s/secret.example.yaml k8s/secret.yaml   # echte Token eintragen (gitignored)
```

**3) Ausrollen**
```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/secret.yaml          # oder: kubectl create secret ... (siehe Datei)
kubectl apply -k k8s/                      # Kern: Ollama, App, Ingest-Job, Modell-Pull

# Warten, bis Modelle geladen und App bereit sind
kubectl -n wiki-rog wait --for=condition=complete job/ollama-pull-models --timeout=1200s
kubectl -n wiki-rog rollout status deploy/wiki-rog-app
```

**4) Wiki indexieren** (der `wiki-rog-ingest`-Job startet automatisch mit `apply -k`).
Erneut indexieren zum Aktualisieren:
```bash
kubectl -n wiki-rog delete job wiki-rog-ingest --ignore-not-found
kubectl apply -f k8s/ingest-job.yaml
```

**5) Web-UI öffnen**
```bash
kubectl -n wiki-rog port-forward svc/wiki-rog-app 8501:80
# -> http://localhost:8501
```

**Optional**
- `kubectl apply -f k8s/ingress.yaml` – Ingress (host/ingressClassName anpassen)
- `kubectl apply -f k8s/ingest-cronjob.yaml` – tägliche Auto-Aktualisierung

**Kleines CPU-Modell:** Standard ist `llama3.2:1b` (~1,3 GB) + `nomic-embed-text`.
Andere Modelle einfach in `k8s/configmap.yaml` (`OLLAMA_LLM_MODEL`) setzen und den
`ollama-pull-models`-Job erneut ausführen. Für Ollama sind bewusst **keine**
GPU-Ressourcen angefordert – es läuft rein auf der CPU.

## Projektstruktur

```
Wiki-rog/
├── cli.py                    # Kommandozeile: test / ingest / ask / chat
├── app.py                    # Streamlit-Web-UI
├── config.py                 # Konfiguration aus .env
├── Dockerfile                # Container-Image
├── requirements.txt
├── .env.example
├── src/
│   ├── bookstack_client.py   # BookStack REST-API
│   ├── chunker.py            # Text-Chunking
│   ├── ollama_client.py      # Ollama (Embeddings + Chat)
│   ├── vectorstore.py        # ChromaDB
│   ├── ingest.py             # Pipeline: fetch → chunk → embed → store
│   └── rag.py                # Retrieval + Antwortgenerierung
└── k8s/                      # Kubernetes-Manifeste
    ├── kustomization.yaml    # Kern-Ressourcen gebündelt
    ├── namespace.yaml
    ├── configmap.yaml        # nicht-geheime Konfiguration + Modellwahl
    ├── secret.example.yaml   # Vorlage für BookStack-Token
    ├── storage.yaml          # PVC für ChromaDB
    ├── ollama.yaml           # Ollama-Deployment (CPU-only) + Service + PVC
    ├── ollama-pull-job.yaml  # lädt die Modelle in Ollama
    ├── app.yaml              # Web-UI Deployment + Service
    ├── ingest-job.yaml       # einmaliger Ingest
    ├── ingest-cronjob.yaml   # optionale Auto-Aktualisierung
    └── ingress.yaml          # optionaler Ingress
```

## Hinweise

- **Datenschutz:** LLM und Embeddings laufen lokal über Ollama; nur die
  BookStack-API wird kontaktiert. Nichts geht an externe Cloud-Dienste.
- **Aktualität:** Nach Änderungen im Wiki einfach `python cli.py ingest`
  ausführen – geänderte Seiten werden neu eingelesen.
- **Andere Modelle:** In `.env` z. B. `OLLAMA_LLM_MODEL=mistral` oder ein
  deutschsprachig starkes Modell setzen. Wenn du das Embedding-Modell
  wechselst, danach `ingest --reset` ausführen.
