"""让 Agent 测试不受开发者本地 .env 中真实模型配置影响。"""

from __future__ import annotations

import os

import pytest

os.environ["EMBEDDING_PROVIDER"] = "local"
os.environ.setdefault("AGENT_SHARED_SECRET", "pytest-agent-secret")


@pytest.fixture
def anyio_backend() -> str:
    """CI 仅安装 asyncio 后端，避免 pytest-anyio 自动参数化未安装的 Trio。"""
    return "asyncio"
