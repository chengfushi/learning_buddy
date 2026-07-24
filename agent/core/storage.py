"""MinIO 对象存储访问；Parser 使用最小权限账号读原件、写派生资产。"""

from __future__ import annotations

import io
from functools import lru_cache

from minio import Minio

from core.config import settings


@lru_cache(maxsize=1)
def client() -> Minio:
    return Minio(
        settings.minio_endpoint,
        access_key=settings.minio_access_key,
        secret_key=settings.minio_secret_key,
        secure=settings.minio_secure,
    )


def ensure_buckets() -> None:
    if not settings.minio_enabled:
        raise RuntimeError("MinIO is not configured")
    for bucket in (settings.minio_source_bucket, settings.minio_derived_bucket):
        if not client().bucket_exists(bucket):
            client().make_bucket(bucket)


def read_source(storage_key: str) -> bytes:
    if not settings.minio_enabled:
        raise RuntimeError("MinIO is not configured")
    response = client().get_object(settings.minio_source_bucket, storage_key)
    try:
        return response.read()
    finally:
        response.close()
        response.release_conn()


def write_derived(storage_key: str, data: bytes, content_type: str) -> None:
    ensure_buckets()
    client().put_object(
        settings.minio_derived_bucket,
        storage_key,
        io.BytesIO(data),
        length=len(data),
        content_type=content_type,
    )
