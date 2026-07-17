"""MinIO 对象存储访问；Parser 使用最小权限账号读原件、写派生资产。"""

from __future__ import annotations

import io
from functools import lru_cache

from minio import Minio

from db import settings


@lru_cache(maxsize=1)
def client() -> Minio:
    """构造并复用 MinIO 客户端。"""
    return Minio(
        settings.minio_endpoint,
        access_key=settings.minio_access_key,
        secret_key=settings.minio_secret_key,
        secure=settings.minio_secure,
    )


def ensure_buckets() -> None:
    """确保本地开发所需 bucket 存在；生产账号可只授予已有 bucket 权限。"""
    if not settings.minio_enabled:
        raise RuntimeError("MinIO is not configured")
    for bucket in (settings.minio_source_bucket, settings.minio_derived_bucket):
        if not client().bucket_exists(bucket):
            client().make_bucket(bucket)


def read_source(storage_key: str) -> bytes:
    """读取 Backend 已授权并写入 source bucket 的原文件。"""
    if not settings.minio_enabled:
        raise RuntimeError("MinIO is not configured")
    response = client().get_object(settings.minio_source_bucket, storage_key)
    try:
        return response.read()
    finally:
        response.close()
        response.release_conn()


def write_derived(storage_key: str, data: bytes, content_type: str) -> None:
    """写入规范 Markdown 或提取图片。"""
    ensure_buckets()
    client().put_object(
        settings.minio_derived_bucket,
        storage_key,
        io.BytesIO(data),
        length=len(data),
        content_type=content_type,
    )
