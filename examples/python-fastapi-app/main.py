"""
Python / FastAPI example: Sentinel REST integration.

Demonstrates:
  - POST /v1/packets/authorize before a sensitive operation
  - Correlation ID propagation
  - Fail-open behaviour when Sentinel is degraded

Install: pip install fastapi uvicorn httpx
Run:
  SENTINEL_API=http://localhost:8080 uvicorn main:app --port 9092
"""

import hashlib
import json
import os
import uuid
from typing import Any, Optional

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse

app = FastAPI(title="Python FastAPI Sentinel Example")

SENTINEL_API = os.getenv("SENTINEL_API", "http://localhost:8080")
APP_ID = "python-billing-api"


async def sentinel_authorize(
    action_name: str,
    category: str,
    risk: str,
    mutating: bool,
    payload: Any = None,
    correlation_id: Optional[str] = None,
) -> dict:
    """
    Call Sentinel /v1/packets/authorize.
    Returns the decision dict. Fails open if Sentinel is unreachable.
    """
    body_str = json.dumps(payload or {}, sort_keys=True)
    body_hash = "sha256:" + hashlib.sha256(body_str.encode()).hexdigest()

    req_body = {
        "app_id": APP_ID,
        "actor_type": "service",
        "action_name": action_name,
        "category": category,
        "risk": risk,
        "mutating": mutating,
        "payload_hash": body_hash,
        "correlation_id": correlation_id or str(uuid.uuid4()),
    }

    try:
        async with httpx.AsyncClient(timeout=3.0) as client:
            resp = await client.post(
                f"{SENTINEL_API}/v1/packets/authorize",
                json=req_body,
            )
            return resp.json()
    except Exception as exc:
        print(f"[sentinel] degraded: {exc}")
        return {"decision": "allow", "decision_id": "degraded", "packet_id": None}


@app.post("/refund")
async def create_refund(request: Request):
    """High-risk mutating refund endpoint governed by Sentinel."""
    correlation_id = request.headers.get("x-correlation-id", str(uuid.uuid4()))
    body = await request.json()

    decision = await sentinel_authorize(
        action_name="invoice.refund.create",
        category="http",
        risk="high",
        mutating=True,
        payload=body,
        correlation_id=correlation_id,
    )

    if decision.get("decision") == "deny":
        return JSONResponse(
            status_code=403,
            content={"error": "denied by sentinel policy", "decision_id": decision.get("decision_id")},
        )

    return JSONResponse(
        status_code=202,
        content={"status": "refund accepted", "packet_id": decision.get("packet_id")},
        headers={"x-correlation-id": correlation_id, "x-sentinel-decision": decision.get("decision", "allow")},
    )


@app.get("/status")
async def status():
    """Low-risk read endpoint."""
    await sentinel_authorize("billing.status.read", "http", "low", False)
    return {"status": "ok"}
