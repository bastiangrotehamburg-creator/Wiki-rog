# syntax=docker/dockerfile:1
FROM python:3.12-slim

# Kein .pyc, ungepufferte Logs
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

# Abhängigkeiten zuerst (bessere Layer-Caches)
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Anwendungscode
COPY config.py cli.py app.py ./
COPY src ./src

# Nicht-root-Benutzer
RUN useradd -m -u 10001 appuser \
    && mkdir -p /data \
    && chown -R appuser:appuser /app /data
USER appuser

# ChromaDB liegt standardmäßig unter /data (per env in k8s gesetzt)
ENV CHROMA_DIR=/data/chroma

EXPOSE 8501

# Standard: Streamlit-Web-UI. Der Ingest-Job überschreibt das Command.
CMD ["streamlit", "run", "app.py", \
     "--server.port=8501", \
     "--server.address=0.0.0.0", \
     "--server.headless=true"]
