# Anleitung: Wiki-Daten „anlernen" (indexieren)

Wiki-rog „lernt" nicht im Sinne eines Trainings – es **indexiert** die
BookStack-Inhalte in einen Vektorspeicher. Bei jeder Frage werden die passenden
Stellen gesucht und dem lokalen LLM als Kontext gegeben. „Anlernen" = einmal
**indexieren**; nach Änderungen im Wiki einfach **erneut indexieren**.

Es gibt drei Wege:

1. [Per Admin-Button in der WebUI](#1-per-admin-button-in-der-webui) ← am bequemsten
2. [Per Kommandozeile (CLI)](#2-per-kommandozeile-cli)
3. [Per Kubernetes-Job](#3-per-kubernetes-job)

---

## 1) Per Admin-Button in der WebUI

In der WebUI gibt es für **Admins** oben die Leiste **„Wiki neu einlesen"**.
Ein Klick startet die Indizierung im Hintergrund; der Status (läuft / fertig /
Fehler, mit Seiten- und Chunk-Anzahl) wird live angezeigt. Mit der Option
**„komplett neu (reset)"** wird der Index vorher geleert.

**Wer ist Admin?**

- **Login-Modus** (mit `access.json`): Nutzer mit `"admin": true`.
- **Offener Modus** (ohne `access.json`): nur wenn `OPEN_ADMIN=true` gesetzt ist.

**Was liest der Button ein?**

- **Offener Modus:** die Standard-Collection (`VECTOR_COLLECTION`) mit dem
  konfigurierten BookStack-Token.
- **Login-/Gruppenmodus:** alle Gruppen aus `access.json`, die ein eigenes
  `bookstack_token_id`/`bookstack_token_secret` hinterlegt haben – jede in ihre
  eigene Collection. So bleibt die Rechtetrennung erhalten.

**Admin einrichten (Login-Modus):**

```bash
# 1) Passwort-Hash erzeugen
./wiki-rog hashpw 'MeinAdminPasswort'

# 2) In config/access.json einen Nutzer mit "admin": true anlegen und den
#    Gruppen die BookStack-Tokens geben (Beispiel: config/access.example.json).
```

`access.json` (Auszug):

```json
{
  "groups": {
    "it": { "collection": "bookstack-it",
            "bookstack_token_id": "…", "bookstack_token_secret": "…" }
  },
  "users": [
    { "username": "admin", "password_hash": "pbkdf2_sha256$…",
      "groups": ["it"], "admin": true }
  ]
}
```

In Kubernetes stehen diese Werte im Secret `wiki-rog-access`
(siehe `k8s/access-secret.example.yaml`); nach Änderung:
`kubectl -n wiki-rog rollout restart deploy/wiki-rog-app`.

---

## 2) Per Kommandozeile (CLI)

```bash
# Verbindungen prüfen (BookStack + Ollama)
./wiki-rog test

# Indexieren (inkrementell – geänderte Seiten werden aktualisiert)
./wiki-rog ingest

# Kompletter Neuaufbau
./wiki-rog ingest --reset
```

**Gruppenweise** (je Gruppe eigenes Token + eigene Collection):

```bash
BOOKSTACK_TOKEN_ID=… BOOKSTACK_TOKEN_SECRET=… VECTOR_COLLECTION=bookstack-it ./wiki-rog ingest --reset
BOOKSTACK_TOKEN_ID=… BOOKSTACK_TOKEN_SECRET=… VECTOR_COLLECTION=bookstack-hr ./wiki-rog ingest --reset
```

Nur bestimmte Bücher/Regale: `BOOKSTACK_BOOK_IDS=1,2` bzw. `BOOKSTACK_SHELF_IDS=3`.

---

## 3) Per Kubernetes-Job

**Einfacher Ingest (eine Collection):**

```bash
kubectl -n wiki-rog delete job wiki-rog-ingest --ignore-not-found
kubectl apply -f k8s/ingest-job.yaml
kubectl -n wiki-rog logs -f job/wiki-rog-ingest
```

**Automatisch (täglich):**

```bash
kubectl apply -f k8s/ingest-cronjob.yaml
```

**Gruppenweise** (je Gruppe eigener Job mit eigenem Token/Collection):

```bash
# Pro Gruppe ein Token-Secret anlegen
kubectl -n wiki-rog create secret generic wiki-rog-token-it \
  --from-literal=BOOKSTACK_TOKEN_ID=… --from-literal=BOOKSTACK_TOKEN_SECRET=…
# Jobs starten (Vorlage anpassen)
kubectl apply -f k8s/ingest-groups-example.yaml
```

---

## Häufige Fragen

- **Wie oft muss ich neu indexieren?** Immer wenn sich Wiki-Inhalte geändert
  haben. Für Automatik: CronJob (K8s) oder ein regelmäßiger CLI-Aufruf.
- **Inkrementell vs. reset?** Ohne `--reset` werden geänderte Seiten aktualisiert
  (alte Chunks der Seite werden ersetzt). `--reset` baut alles neu auf –
  nötig z. B. nach Wechsel des Embedding-Modells.
- **Antwort sagt „Das steht nicht im Wiki."** Entweder wirklich nicht vorhanden,
  oder noch nicht indexiert → einmal einlesen. Bei Login: prüfen, ob der Nutzer
  in der richtigen Gruppe ist.
- **Der Button fehlt.** Nur Admins sehen ihn (siehe oben). Im offenen Modus
  `OPEN_ADMIN=true` setzen.
- **Der Button meldet „keine Indizierungsziele konfiguriert".** Im Login-Modus
  hat keine Gruppe ein `bookstack_token_id` hinterlegt → Tokens ergänzen oder
  per K8s-Job (Weg 3) indexieren.
