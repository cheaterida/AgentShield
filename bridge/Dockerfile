FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY langtrace_bridge.py .
ENTRYPOINT ["python", "langtrace_bridge.py"]
