"""应用级 HTTP 客户端、连接池和受控重试。

所有外部模型调用共享客户端，避免每个请求重复建立连接。
"""

from __future__ import annotations

import asyncio
import threading
import time
from collections.abc import Mapping
from typing import Any

import httpx

from core.config import settings

_async_client: httpx.AsyncClient | None = None
_sync_client: httpx.Client | None = None
_async_lock = asyncio.Lock()
_sync_lock = threading.Lock()
_async_slots = asyncio.Semaphore(32)
_sync_slots = threading.BoundedSemaphore(32)


class ExternalServiceUnavailable(RuntimeError):
    """外部服务熔断时的快速失败错误。"""


class _Circuit:
    def __init__(self) -> None:
        self.failures = 0
        self.opened_at = 0.0


_circuits: dict[str, _Circuit] = {}
_circuits_lock = threading.Lock()


def _circuit_key(url: str) -> str:
    return url.split("/", 3)[2] if "://" in url else url


def _circuit_allows(url: str) -> bool:
    key = _circuit_key(url)
    with _circuits_lock:
        circuit = _circuits.setdefault(key, _Circuit())
        if not circuit.opened_at:
            return True
        if time.monotonic() - circuit.opened_at >= settings.external_circuit_reset_s:
            circuit.opened_at = 0.0
            circuit.failures = 0
            return True
        return False


def _record(url: str, success: bool) -> None:
    key = _circuit_key(url)
    with _circuits_lock:
        circuit = _circuits.setdefault(key, _Circuit())
        if success:
            circuit.failures = 0
            circuit.opened_at = 0.0
        else:
            circuit.failures += 1
            if circuit.failures >= settings.external_circuit_failures:
                circuit.opened_at = time.monotonic()


def init_sync_client() -> httpx.Client:
    global _sync_client
    with _sync_lock:
        if _sync_client is None:
            _sync_client = httpx.Client(
                limits=httpx.Limits(
                    max_connections=settings.external_max_connections,
                    max_keepalive_connections=settings.external_keepalive_connections,
                )
            )
        return _sync_client


async def init_async_client() -> httpx.AsyncClient:
    global _async_client
    async with _async_lock:
        if _async_client is None:
            _async_client = httpx.AsyncClient(
                limits=httpx.Limits(
                    max_connections=settings.external_max_connections,
                    max_keepalive_connections=settings.external_keepalive_connections,
                )
            )
        return _async_client


def get_sync_client() -> httpx.Client:
    if _sync_client is None:
        return init_sync_client()
    return _sync_client


def get_async_client() -> httpx.AsyncClient:
    if _async_client is None:
        raise RuntimeError("async HTTP client is not initialized")
    return _async_client


async def close_clients() -> None:
    global _async_client, _sync_client
    async_client = _async_client
    _async_client = None
    if async_client is not None:
        await async_client.aclose()
    with _sync_lock:
        sync_client = _sync_client
        _sync_client = None
    if sync_client is not None:
        sync_client.close()


def _request_kwargs(
    headers: Mapping[str, str] | None,
    json: Any,
    timeout: float,
) -> dict[str, Any]:
    return {"headers": headers, "json": json, "timeout": timeout}


def post_sync(
    url: str,
    *,
    headers: Mapping[str, str] | None,
    json: Any,
    timeout: float,
) -> httpx.Response:
    if not _circuit_allows(url):
        raise ExternalServiceUnavailable(f"external service circuit open: {_circuit_key(url)}")
    last_error: Exception | None = None
    for attempt in range(settings.external_retry_attempts + 1):
        try:
            with _sync_slots:
                response = get_sync_client().post(url, **_request_kwargs(headers, json, timeout))
            status_code = getattr(response, "status_code", 200)
            if status_code >= 500:
                raise httpx.HTTPStatusError(
                    f"external service returned {status_code}",
                    request=response.request,
                    response=response,
                )
            response.raise_for_status()
            _record(url, True)
            return response
        except (httpx.TransportError, httpx.HTTPStatusError) as exc:
            last_error = exc
            if isinstance(exc, httpx.HTTPStatusError) and exc.response.status_code < 500:
                raise
            if attempt >= settings.external_retry_attempts:
                break
            time.sleep(settings.external_retry_backoff_s * (2**attempt))
    _record(url, False)
    if last_error is not None:
        raise last_error
    raise ExternalServiceUnavailable("external service request failed")


async def post_async(
    url: str,
    *,
    headers: Mapping[str, str] | None,
    json: Any,
    timeout: float,
) -> httpx.Response:
    if not _circuit_allows(url):
        raise ExternalServiceUnavailable(f"external service circuit open: {_circuit_key(url)}")
    last_error: Exception | None = None
    for attempt in range(settings.external_retry_attempts + 1):
        try:
            async with _async_slots:
                response = await get_async_client().post(
                    url, **_request_kwargs(headers, json, timeout)
                )
            status_code = getattr(response, "status_code", 200)
            if status_code >= 500:
                raise httpx.HTTPStatusError(
                    f"external service returned {status_code}",
                    request=response.request,
                    response=response,
                )
            response.raise_for_status()
            _record(url, True)
            return response
        except (httpx.TransportError, httpx.HTTPStatusError) as exc:
            last_error = exc
            if isinstance(exc, httpx.HTTPStatusError) and exc.response.status_code < 500:
                raise
            if attempt >= settings.external_retry_attempts:
                break
            await asyncio.sleep(settings.external_retry_backoff_s * (2**attempt))
    _record(url, False)
    if last_error is not None:
        raise last_error
    raise ExternalServiceUnavailable("external service request failed")
