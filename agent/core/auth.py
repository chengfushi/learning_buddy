"""Backend→Agent 服务间共享密钥认证。"""

from __future__ import annotations

import secrets
from typing import Annotated

from fastapi import Header, HTTPException, status

from core.config import settings

AGENT_TOKEN_HEADER = "X-Agent-Token"


def assert_agent_auth_config() -> None:
    if not settings.agent_shared_secret.strip():
        raise RuntimeError("AGENT_SHARED_SECRET 未配置，Agent 拒绝以无认证模式启动")


def require_agent_token(
    x_agent_token: Annotated[str | None, Header(alias=AGENT_TOKEN_HEADER)] = None,
) -> None:
    expected = settings.agent_shared_secret
    if not expected or x_agent_token is None or not secrets.compare_digest(x_agent_token, expected):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="invalid agent service credential",
        )
