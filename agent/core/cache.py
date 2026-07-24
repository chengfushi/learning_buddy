"""Redis 非权限缓存；不可用时所有调用透明降级。"""

from __future__ import annotations

import hashlib
import json

from redis.asyncio import Redis

from core.config import settings

_redis: Redis | None = None


def cache_key(namespace: str, payload: object) -> str:
    raw = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    digest = hashlib.sha256(raw.encode()).hexdigest()
    return f"rag:{namespace}:{digest}"


def client() -> Redis:
    global _redis
    if _redis is None:
        _redis = Redis.from_url(
            f"redis://{settings.redis_addr}",
            decode_responses=True,
            socket_connect_timeout=settings.redis_timeout_s,
            socket_timeout=settings.redis_timeout_s,
            retry_on_timeout=False,
        )
    return _redis


async def get_json(key: str) -> dict[str, object] | list[object] | None:
    try:
        value = await client().get(key)
        parsed: object = json.loads(value) if value else None
        return parsed if isinstance(parsed, (dict, list)) else None
    except Exception:
        return None


async def set_json(key: str, value: object, ttl_s: int) -> None:
    try:
        await client().set(key, json.dumps(value, ensure_ascii=False), ex=ttl_s)
    except Exception:
        return
