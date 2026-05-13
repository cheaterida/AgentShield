# ── AgentShield ML Pipeline Docker Image ──
# Serves the 150D embedding, CFG analysis, and GNN anomaly detection API.
#
# Build:  docker build -t agentshield/ml-pipeline:latest -f ml-pipeline/Dockerfile .
# Run:    docker run -d -p 8090:8090 -v ./models:/app/models agentshield/ml-pipeline:latest

FROM pytorch/pytorch:2.4.0-cuda12.4-runtime

# Install DGL (CPU version for broad compatibility)
RUN pip install --no-cache-dir \
    dgl>=2.3 \
    fastapi>=0.115 \
    "uvicorn[standard]>=0.30" \
    pydantic>=2.9 \
    pydantic-settings>=2.6

WORKDIR /app

# Copy source
COPY ml-pipeline/pyproject.toml /app/
COPY ml-pipeline/src/ /app/src/

# Install the package in development mode
RUN pip install -e . --no-deps

# Create model storage directory
RUN mkdir -p /app/models /app/policies

# Pre-download or init any model weights (cold start — models trained on first use)
ENV AGENTSHIELD_ML_MODEL_PATH=/app/models/cae_checkpoint.pt

EXPOSE 8090
CMD ["uvicorn", "agentshield_ml.api.main:app", "--host", "0.0.0.0", "--port", "8090"]
