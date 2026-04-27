"""
OpenAI-compatible AI lane example.

This example wraps any OpenAI-compatible API endpoint with Sentinel AI governance:
  1. POST /v1/ai/authorize before the model call (prompt hash, model hash)
  2. Call the upstream model API
  3. POST /v1/ai/result after the call (response hash, tool call count)

Install: pip install openai httpx
Run:
  SENTINEL_API=http://localhost:8080 \
  OPENAI_API_KEY=... \
  python main.py
"""

import hashlib
import json
import os
import uuid

import httpx

SENTINEL_API = os.getenv("SENTINEL_API", "http://localhost:8080")
OPENAI_API_BASE = os.getenv("OPENAI_API_BASE", "https://api.openai.com/v1")
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
APP_ID = "openai-billing-agent"


def sha256_hash(data: str) -> str:
    return "sha256:" + hashlib.sha256(data.encode()).hexdigest()


def sentinel_ai_authorize(model_id: str, prompt: str, correlation_id: str) -> dict:
    payload = {
        "app_id": APP_ID,
        "correlation_id": correlation_id,
        "model_id_hash": sha256_hash(model_id),
        "prompt_hash": sha256_hash(prompt),
        "tool_call_count": 0,
    }
    try:
        resp = httpx.post(f"{SENTINEL_API}/v1/ai/authorize", json=payload, timeout=3.0)
        return resp.json()
    except Exception as exc:
        print(f"[sentinel] ai/authorize degraded: {exc}")
        return {"decision": "allow", "decision_id": "degraded", "packet_id": None}


def sentinel_ai_result(packet_id: str, response: str, tool_call_count: int, correlation_id: str):
    payload = {
        "app_id": APP_ID,
        "correlation_id": correlation_id,
        "packet_id": packet_id,
        "response_hash": sha256_hash(response),
        "tool_call_count": tool_call_count,
    }
    try:
        httpx.post(f"{SENTINEL_API}/v1/ai/result", json=payload, timeout=3.0)
    except Exception as exc:
        print(f"[sentinel] ai/result degraded: {exc}")


def governed_chat_completion(model: str, messages: list) -> dict:
    """
    A governed wrapper around an OpenAI-compatible chat completion.
    1. Authorize the prompt with Sentinel.
    2. Call the upstream model.
    3. Record the result with Sentinel.
    """
    correlation_id = str(uuid.uuid4())
    prompt_text = json.dumps(messages)

    # Step 1: authorize
    decision = sentinel_ai_authorize(model, prompt_text, correlation_id)
    if decision.get("decision") == "deny":
        raise PermissionError(f"AI call denied by Sentinel policy: {decision.get('decision_id')}")

    packet_id = decision.get("packet_id")

    # Step 2: call upstream model
    resp = httpx.post(
        f"{OPENAI_API_BASE}/chat/completions",
        headers={"Authorization": f"Bearer {OPENAI_API_KEY}", "Content-Type": "application/json"},
        json={"model": model, "messages": messages},
        timeout=60.0,
    )
    resp.raise_for_status()
    result = resp.json()

    # Step 3: record result
    response_text = result.get("choices", [{}])[0].get("message", {}).get("content", "")
    tool_call_count = len(result.get("choices", [{}])[0].get("message", {}).get("tool_calls", []))
    sentinel_ai_result(packet_id, response_text, tool_call_count, correlation_id)

    return result


if __name__ == "__main__":
    result = governed_chat_completion(
        model="gpt-4o-mini",
        messages=[{"role": "user", "content": "Summarise the refund policy in one sentence."}],
    )
    print(json.dumps(result, indent=2))
