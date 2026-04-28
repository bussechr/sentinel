"""Sentinel Python SDK — sync and async HTTP clients.

Surface:

  - SentinelClient.authorize           POST /v1/packets/authorize
  - SentinelClient.ai_authorize        POST /v1/ai/authorize
  - SentinelClient.ai_result           POST /v1/ai/result
  - SentinelClient.rewind              GET  /v1/evidence/rewind/{cid}
  - SentinelClient.causal_graph        GET  /v1/evidence/causal/{cid}
  - SentinelClient.list_writers        GET  /v1/ledger/writers
  - SentinelClient.shadow_divergences  GET  /v1/policy/shadow/divergences

Both sync and async (AsyncSentinelClient) variants share the same
configuration and request shapes.
"""

from __future__ import annotations

import dataclasses
import hashlib
import uuid
from datetime import datetime
from typing import Any, Iterable, Mapping, Optional

import httpx

AI_ROUTE_HEADER = "X-Sentinel-AI-Route"
AI_ACTOR_HEADER = "X-Sentinel-AI-Actor"
AI_ROUTE_VALUE = "ai-gateway"


def sha256_hash(value: str | bytes) -> str:
    """Compute the canonical sha256:<hex> hash used by Sentinel payloads."""
    if isinstance(value, str):
        value = value.encode("utf-8")
    return "sha256:" + hashlib.sha256(value).hexdigest()


class SentinelError(RuntimeError):
    """Raised when Sentinel returns a non-2xx response in guard/enforce mode."""


@dataclasses.dataclass(frozen=True)
class RoutePolicy:
    action_name: str
    category: str
    risk: str
    mutating: bool = False
    resource_type: str = ""


@dataclasses.dataclass(frozen=True)
class AuthorizeResponse:
    decision: str
    decision_id: str
    packet_id: str
    reason: str = ""
    ledger_required: bool = False
    receipt_id: str = ""
    receipt_status: str = ""
    witness_id: str = ""

    @classmethod
    def from_json(cls, body: Mapping[str, Any]) -> "AuthorizeResponse":
        return cls(
            decision=str(body.get("decision", "")),
            decision_id=str(body.get("decision_id", "")),
            packet_id=str(body.get("packet_id", "")),
            reason=str(body.get("reason", "")),
            ledger_required=bool(body.get("ledger_required", False)),
            receipt_id=str(body.get("receipt_id", "")),
            receipt_status=str(body.get("receipt_status", "")),
            witness_id=str(body.get("witness_id", "")),
        )


# ─────────────────────────────────── Sync ────────────────────────────────────


class SentinelClient:
    """Synchronous Sentinel client.

    Use :class:`AsyncSentinelClient` from asyncio code.
    """

    def __init__(
        self,
        endpoint: str,
        app_id: str,
        mode: str = "observe",
        token: Optional[str] = None,
        timeout: float = 5.0,
        client: Optional[httpx.Client] = None,
    ) -> None:
        self.endpoint = endpoint.rstrip("/")
        self.app_id = app_id
        self.mode = mode
        self.token = token
        self.timeout = timeout
        self._client = client or httpx.Client(timeout=timeout)
        self._owned = client is None

    # ─── lifecycle ───
    def close(self) -> None:
        if self._owned:
            self._client.close()

    def __enter__(self) -> "SentinelClient":
        return self

    def __exit__(self, *_exc: Any) -> None:
        self.close()

    # ─── core endpoints ───
    def authorize(
        self,
        correlation_id: str,
        policy: RoutePolicy,
        payload_hash: str,
        actor_type: str = "service",
        actor_id_hash: str = "",
        trace_id: str = "",
    ) -> AuthorizeResponse:
        body = {
            "app_id": self.app_id,
            "actor_type": actor_type,
            "actor_id_hash": actor_id_hash,
            "action_name": policy.action_name,
            "category": policy.category,
            "risk": policy.risk,
            "mutating": policy.mutating,
            "resource_type": policy.resource_type,
            "payload_hash": payload_hash,
            "correlation_id": correlation_id,
            "trace_id": trace_id,
        }
        try:
            data = self._post("/v1/packets/authorize", body)
        except Exception as exc:  # noqa: BLE001
            if self.mode == "observe":
                return AuthorizeResponse(
                    decision="allow",
                    decision_id="degraded",
                    packet_id="",
                    reason=f"sentinel degraded: {exc}",
                )
            raise
        return AuthorizeResponse.from_json(data)

    def ai_authorize(
        self,
        correlation_id: str,
        model_id_hash: str,
        prompt_hash: str,
        tool_call_count: int = 0,
    ) -> AuthorizeResponse:
        body = {
            "app_id": self.app_id,
            "correlation_id": correlation_id,
            "model_id_hash": model_id_hash,
            "prompt_hash": prompt_hash,
            "tool_call_count": tool_call_count,
        }
        data = self._post("/v1/ai/authorize", body, ai_lane=True)
        return AuthorizeResponse.from_json(data)

    def ai_result(
        self,
        correlation_id: str,
        response_hash: str,
        tool_call_count: int,
        packet_id: Optional[str] = None,
    ) -> Mapping[str, Any]:
        body = {
            "app_id": self.app_id,
            "correlation_id": correlation_id,
            "packet_id": packet_id or "",
            "response_hash": response_hash,
            "tool_call_count": tool_call_count,
        }
        return self._post("/v1/ai/result", body, ai_lane=True)

    def rewind(self, correlation_id: str, window: Optional[str] = None) -> Mapping[str, Any]:
        path = f"/v1/evidence/rewind/{correlation_id}"
        params = {"window": window} if window else None
        return self._get(path, params=params)

    def causal_graph(self, correlation_id: str) -> Mapping[str, Any]:
        return self._get(f"/v1/evidence/causal/{correlation_id}")

    def list_writers(self) -> Mapping[str, Any]:
        return self._get("/v1/ledger/writers")

    def shadow_divergences(
        self,
        since: Optional[datetime] = None,
        limit: int = 50,
    ) -> Mapping[str, Any]:
        params: dict[str, Any] = {"limit": limit}
        if since is not None:
            params["since"] = since.isoformat()
        return self._get("/v1/policy/shadow/divergences", params=params)

    # ─── helpers ───
    @staticmethod
    def new_correlation_id() -> str:
        return f"corr_{uuid.uuid4()}"

    # ─── transport ───
    def _post(self, path: str, body: Mapping[str, Any], ai_lane: bool = False) -> Mapping[str, Any]:
        resp = self._client.post(
            self.endpoint + path,
            json=body,
            headers=self._headers(ai_lane),
        )
        return _json_or_raise(resp, path)

    def _get(self, path: str, params: Optional[Mapping[str, Any]] = None) -> Mapping[str, Any]:
        resp = self._client.get(
            self.endpoint + path,
            params=params,
            headers=self._headers(False),
        )
        return _json_or_raise(resp, path)

    def _headers(self, ai_lane: bool) -> dict[str, str]:
        h: dict[str, str] = {"Content-Type": "application/json"}
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        if ai_lane:
            h[AI_ROUTE_HEADER] = AI_ROUTE_VALUE
            h[AI_ACTOR_HEADER] = "model"
        return h


# ─────────────────────────────────── Async ───────────────────────────────────


class AsyncSentinelClient:
    """Asyncio variant of :class:`SentinelClient`."""

    def __init__(
        self,
        endpoint: str,
        app_id: str,
        mode: str = "observe",
        token: Optional[str] = None,
        timeout: float = 5.0,
        client: Optional[httpx.AsyncClient] = None,
    ) -> None:
        self.endpoint = endpoint.rstrip("/")
        self.app_id = app_id
        self.mode = mode
        self.token = token
        self.timeout = timeout
        self._client = client or httpx.AsyncClient(timeout=timeout)
        self._owned = client is None

    async def aclose(self) -> None:
        if self._owned:
            await self._client.aclose()

    async def __aenter__(self) -> "AsyncSentinelClient":
        return self

    async def __aexit__(self, *_exc: Any) -> None:
        await self.aclose()

    async def authorize(
        self,
        correlation_id: str,
        policy: RoutePolicy,
        payload_hash: str,
        actor_type: str = "service",
        actor_id_hash: str = "",
        trace_id: str = "",
    ) -> AuthorizeResponse:
        body = {
            "app_id": self.app_id,
            "actor_type": actor_type,
            "actor_id_hash": actor_id_hash,
            "action_name": policy.action_name,
            "category": policy.category,
            "risk": policy.risk,
            "mutating": policy.mutating,
            "resource_type": policy.resource_type,
            "payload_hash": payload_hash,
            "correlation_id": correlation_id,
            "trace_id": trace_id,
        }
        try:
            data = await self._post("/v1/packets/authorize", body)
        except Exception as exc:  # noqa: BLE001
            if self.mode == "observe":
                return AuthorizeResponse(
                    decision="allow",
                    decision_id="degraded",
                    packet_id="",
                    reason=f"sentinel degraded: {exc}",
                )
            raise
        return AuthorizeResponse.from_json(data)

    async def ai_authorize(
        self,
        correlation_id: str,
        model_id_hash: str,
        prompt_hash: str,
        tool_call_count: int = 0,
    ) -> AuthorizeResponse:
        body = {
            "app_id": self.app_id,
            "correlation_id": correlation_id,
            "model_id_hash": model_id_hash,
            "prompt_hash": prompt_hash,
            "tool_call_count": tool_call_count,
        }
        data = await self._post("/v1/ai/authorize", body, ai_lane=True)
        return AuthorizeResponse.from_json(data)

    async def ai_result(
        self,
        correlation_id: str,
        response_hash: str,
        tool_call_count: int,
        packet_id: Optional[str] = None,
    ) -> Mapping[str, Any]:
        body = {
            "app_id": self.app_id,
            "correlation_id": correlation_id,
            "packet_id": packet_id or "",
            "response_hash": response_hash,
            "tool_call_count": tool_call_count,
        }
        return await self._post("/v1/ai/result", body, ai_lane=True)

    async def rewind(self, correlation_id: str, window: Optional[str] = None) -> Mapping[str, Any]:
        params = {"window": window} if window else None
        return await self._get(f"/v1/evidence/rewind/{correlation_id}", params=params)

    async def causal_graph(self, correlation_id: str) -> Mapping[str, Any]:
        return await self._get(f"/v1/evidence/causal/{correlation_id}")

    async def list_writers(self) -> Mapping[str, Any]:
        return await self._get("/v1/ledger/writers")

    async def shadow_divergences(
        self,
        since: Optional[datetime] = None,
        limit: int = 50,
    ) -> Mapping[str, Any]:
        params: dict[str, Any] = {"limit": limit}
        if since is not None:
            params["since"] = since.isoformat()
        return await self._get("/v1/policy/shadow/divergences", params=params)

    async def _post(
        self,
        path: str,
        body: Mapping[str, Any],
        ai_lane: bool = False,
    ) -> Mapping[str, Any]:
        resp = await self._client.post(
            self.endpoint + path,
            json=body,
            headers=self._headers(ai_lane),
        )
        return _json_or_raise(resp, path)

    async def _get(
        self,
        path: str,
        params: Optional[Mapping[str, Any]] = None,
    ) -> Mapping[str, Any]:
        resp = await self._client.get(
            self.endpoint + path,
            params=params,
            headers=self._headers(False),
        )
        return _json_or_raise(resp, path)

    def _headers(self, ai_lane: bool) -> dict[str, str]:
        h: dict[str, str] = {"Content-Type": "application/json"}
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        if ai_lane:
            h[AI_ROUTE_HEADER] = AI_ROUTE_VALUE
            h[AI_ACTOR_HEADER] = "model"
        return h


# ─────────────────────────────── helpers ───────────────────────────────


def _json_or_raise(resp: httpx.Response, path: str) -> Mapping[str, Any]:
    if resp.status_code >= 400:
        raise SentinelError(f"{path} -> {resp.status_code}: {resp.text}")
    if not resp.content:
        return {}
    return resp.json()


def _all() -> Iterable[str]:  # pragma: no cover
    return (
        "SentinelClient",
        "AsyncSentinelClient",
        "RoutePolicy",
        "AuthorizeResponse",
        "SentinelError",
        "sha256_hash",
        "AI_ROUTE_HEADER",
        "AI_ACTOR_HEADER",
        "AI_ROUTE_VALUE",
    )
