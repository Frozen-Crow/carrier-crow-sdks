"""Synchronous Carrier Crow API client; wire field names are preserved."""

from __future__ import annotations

import hashlib
import hmac
import json
import math
import os
import random
import re
import time
import uuid
from typing import Any, Callable, Iterator, Mapping, Optional, cast
from urllib.parse import quote

import httpx

from .models import (
    BatchResponse,
    Domain,
    EmailDetail,
    EmailList,
    EmailQuery,
    EmailSummary,
    SendEmail,
    SendResult,
    WebhookBody,
)

__version__ = "0.1.0"


class CarrierCrowError(Exception):
    def __init__(
        self,
        message: str,
        *,
        status: int = 0,
        code: str = "sdk_error",
        field: Optional[str] = None,
        retry_after: Optional[float] = None,
    ):
        super().__init__(message)
        self.message = message
        self.status = status
        self.code = code
        self.field = field
        self.retry_after = retry_after

    @property
    def retryAfter(self) -> Optional[float]:
        return self.retry_after


class Webhooks:
    @staticmethod
    def verify(
        body: bytes | str,
        signature_header: str,
        secret: str,
        tolerance: float = 300,
        *,
        now: Optional[float] = None,
    ) -> WebhookBody:
        raw = body.encode("utf-8") if isinstance(body, str) else body
        fields = [part.strip().partition("=") for part in signature_header.split(",")]
        timestamps = [value for name, sep, value in fields if name == "t" and sep]
        signatures = [value for name, sep, value in fields if name == "v1" and sep]
        if not timestamps or not re.fullmatch(r"[0-9]+", timestamps[0]):
            raise CarrierCrowError(
                "Malformed webhook signature", code="invalid_signature"
            )
        timestamp = timestamps[0]
        if (
            not math.isfinite(tolerance)
            or (now is not None and not math.isfinite(now))
            or tolerance < 0
            or abs((int(time.time()) if now is None else now) - int(timestamp))
            > tolerance
        ):
            raise CarrierCrowError(
                "Webhook timestamp outside tolerance", code="invalid_signature"
            )
        expected = hmac.new(
            secret.encode(), timestamp.encode() + b"." + raw, hashlib.sha256
        ).digest()
        matches = [
            hmac.compare_digest(expected, bytes.fromhex(value))
            for value in signatures
            if re.fullmatch(r"[0-9a-f]{64}", value)
        ]
        if not any(matches):
            raise CarrierCrowError(
                "Webhook signature does not match", code="invalid_signature"
            )
        try:
            parsed = json.loads(raw)
            if (
                not isinstance(parsed, dict)
                or not isinstance(parsed.get("webhook"), str)
                or not isinstance(parsed.get("delivery"), str)
                or not isinstance(parsed.get("events"), list)
            ):
                raise ValueError("Invalid webhook envelope")
            return cast(WebhookBody, parsed)
        except (ValueError, UnicodeError) as exc:
            raise CarrierCrowError(
                "Webhook body is not JSON", code="invalid_json"
            ) from exc


webhooks = Webhooks()


class Emails:
    def __init__(self, client: CarrierCrow):
        self._client = client

    def send(
        self,
        params: SendEmail,
        *,
        idempotency_key: Optional[str] = None,
        retry: bool = False,
    ) -> SendResult:
        return cast(
            SendResult,
            self._client._request(
                "POST",
                "/v1/emails",
                body=params,
                key=idempotency_key,
                send=True,
                retry=retry,
            ),
        )

    def batch(
        self,
        emails: list[SendEmail],
        *,
        idempotency_key: Optional[str] = None,
        retry: bool = False,
    ) -> BatchResponse:
        return cast(
            BatchResponse,
            self._client._request(
                "POST",
                "/v1/emails/batch",
                body={"emails": emails},
                key=idempotency_key,
                send=True,
                retry=retry,
            ),
        )

    def get(self, email_id: str) -> EmailDetail:
        return cast(
            EmailDetail,
            self._client._request("GET", "/v1/emails/" + quote(email_id, safe="")),
        )

    def list(self, query: Optional[EmailQuery] = None) -> EmailList:
        return cast(EmailList, self._client._request("GET", "/v1/emails", query=query))

    def pages(self, query: Optional[EmailQuery] = None) -> Iterator[EmailList]:
        query = (query or {}).copy()
        seen = {query["before"]} if "before" in query else set()
        while True:
            page = self.list(query)
            yield page
            if not page["next"]:
                return
            if page["next"] in seen:
                raise CarrierCrowError(
                    "Repeated paging cursor", code="invalid_response"
                )
            seen.add(page["next"])
            query["before"] = page["next"]

    def iter(self, query: Optional[EmailQuery] = None) -> Iterator[EmailSummary]:
        for page in self.pages(query):
            yield from page["emails"]


class Domains:
    def __init__(self, client: CarrierCrow):
        self._client = client

    def list(self) -> dict[str, list[Domain]]:
        return cast(
            dict[str, list[Domain]], self._client._request("GET", "/v1/domains")
        )

    def get(self, domain_id: str) -> Domain:
        return cast(
            Domain,
            self._client._request("GET", "/v1/domains/" + quote(domain_id, safe="")),
        )

    def check(self, domain_id: str) -> Domain:
        return cast(
            Domain,
            self._client._request(
                "POST", "/v1/domains/" + quote(domain_id, safe="") + "/check"
            ),
        )


class CarrierCrow:
    def __init__(
        self,
        api_key: Optional[str] = None,
        *,
        base_url: str = "https://api.mta.carriercrow.com",
        max_retries: int = 2,
        timeout: float = 30,
        backoff: float = 0.5,
        max_retry_after: float = 60,
        sleep: Callable[[float], None] = time.sleep,
    ):
        self.api_key = (
            api_key
            if api_key is not None
            else os.environ.get("CARRIERCROW_API_KEY", "")
        )
        if not self.api_key:
            raise CarrierCrowError("API key is required", code="missing_api_key")
        self.base_url = base_url.rstrip("/")
        if max_retries < 0 or backoff < 0:
            raise ValueError("max_retries and backoff must be nonnegative")
        if not math.isfinite(max_retry_after) or max_retry_after < 0:
            raise ValueError("max_retry_after must be finite and nonnegative")
        self.max_retry_after = max_retry_after
        self.max_retries = max_retries
        self.backoff = backoff
        self._sleep = sleep
        self._http = httpx.Client(timeout=timeout, follow_redirects=False)
        self.emails = Emails(self)
        self.domains = Domains(self)
        self.webhooks = webhooks

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> CarrierCrow:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def _request(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        query: Optional[Mapping[str, Any]] = None,
        key: Optional[str] = None,
        send: bool = False,
        retry: bool = False,
    ) -> Any:
        if retry and not key:
            key = str(uuid.uuid4())
        headers = {
            "Authorization": "Bearer " + self.api_key,
            "User-Agent": "carriercrow-python/" + __version__,
            "Accept": "application/json",
        }
        if key:
            headers["Idempotency-Key"] = key
        allowed = not send or bool(key)
        for attempt in range(self.max_retries + 1):
            retry_after = None
            try:
                response = self._http.request(
                    method,
                    self.base_url + path,
                    headers=headers,
                    params=query,
                    json=body,
                )
            except httpx.TransportError as exc:
                error = CarrierCrowError(str(exc), code="network_error")
            else:
                try:
                    payload = response.json()
                except ValueError as exc:
                    if response.is_success:
                        raise CarrierCrowError(
                            "Invalid JSON response",
                            status=response.status_code,
                            code="invalid_response",
                        ) from exc
                    payload = {}
                if response.is_success:
                    return payload
                try:
                    retry_after = float(response.headers["Retry-After"])
                    if not math.isfinite(retry_after) or retry_after < 0:
                        retry_after = None
                except (KeyError, ValueError):
                    pass
                if not isinstance(payload, dict):
                    payload = {}
                error = CarrierCrowError(
                    payload.get("error", response.reason_phrase),
                    status=response.status_code,
                    code=payload.get("code", "http_error"),
                    field=payload.get("field"),
                    retry_after=retry_after,
                )
                if response.status_code not in (429, 503):
                    raise error
            if (
                not allowed
                or attempt == self.max_retries
                or (retry_after is not None and retry_after > self.max_retry_after)
            ):
                raise error
            delay = self.backoff * (2**attempt) * (0.5 + random.random())
            self._sleep(max(delay, retry_after or 0))
        raise AssertionError("unreachable")
