"""通用工具函数。"""

from __future__ import annotations

import re

_ACCESS_KEY_PATTERN = re.compile(r"\bAKIA[0-9A-Z]{16}\b")


def redact_for_cloud(text: str) -> str:
    patterns = [
        (r"(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{12,}", "Bearer [REDACTED]"),
        (
            r"(?i)\b((?:(?:aws[_-]?)?(?:access[_-]?key(?:[_-]?id)?|"
            r"secret[_-]?access[_-]?key|session[_-]?token)|api[_-]?key|secret(?:[_-]?key)?|"
            r"auth[_-]?token|refresh[_-]?token|password|passwd|pwd)\s*[:=]\s*)"
            r"[\"']?[A-Za-z0-9_./+=:@-]{8,}[\"']?",
            r"\1[REDACTED]",
        ),
        (r"\bAKIA[0-9A-Z]{16}\b", "[REDACTED_AWS_ACCESS_KEY]"),
        (r"(?i)\b(?:sk|api)[-_][A-Za-z0-9_-]{12,}\b", "[REDACTED_KEY]"),
        (r"\b[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}\b", "[REDACTED_EMAIL]"),
        (r"(?<!\d)1[3-9]\d{9}(?!\d)", "[REDACTED_PHONE]"),
    ]
    redacted = text
    for expression, replacement in patterns:
        redacted = re.sub(expression, replacement, redacted)
    return redacted


def estimate_tokens(text: str) -> int:
    cjk = len(re.findall(r"[\u4e00-\u9fff]", text))
    latin = len(re.findall(r"[A-Za-z0-9_./:-]+", text))
    return cjk + latin
