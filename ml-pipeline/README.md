# ml-pipeline

Python 服务与离线训练作业入口；依赖见 `pyproject.toml`。

```bash
pip install -e ".[dev]"
uvicorn agentshield_ml.api.main:app --reload --port 8090
```
