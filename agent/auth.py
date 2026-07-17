"""Backend→Agent 服务间共享密钥认证（engineering-standards R5）。"""

from __future__ import annotations

import secrets
from typing import Annotated

from fastapi import Header, HTTPException, status

from db import settings

AGENT_TOKEN_HEADER = "X-Agent-Token"


def assert_agent_auth_config() -> None:
    """启动时强制要求共享密钥，避免服务在未认证模式下运行。"""
    if not settings.agent_shared_secret.strip():
        raise RuntimeError("AGENT_SHARED_SECRET 未配置，Agent 拒绝以无认证模式启动")


def require_agent_token(
    x_agent_token: Annotated[str | None, Header(alias=AGENT_TOKEN_HEADER)] = None,
) -> None:
    """校验 Backend 注入的共享密钥；缺失、错误或服务端未配置时均拒绝。"""
    expected = settings.agent_shared_secret
    if not expected or x_agent_token is None or not secrets.compare_digest(x_agent_token, expected):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="invalid agent service credential",
        )
