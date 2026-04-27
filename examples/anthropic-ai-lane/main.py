"""
Anthropic/Claude AI lane example.

This example wraps the Anthropic API with Sentinel AI governance:
  1. POST /v1/ai/authorize before the model call (prompt hash, model hash)
  2. Call the upstream Claude API
  3. POST /v1/ai/result after the call (response hash, tool call count)

Install: pip install anthropic httpx
Run:
  SENTINEL_API=http://localhost:8080 \
  ANTHROPIC_API_KEY=... \
  python main.py
"""

import hashlib
import json
import os
import uuid

import httpx
import anthropic

SENTINEL_API = os.getenv("SENTINEL_API", "http://localhost:8080")
ANTHROPIC_API_KEY = os.getenv("ANTHROPIC_API_KEY", "")
APP_ID = "claude-billing-agent"

client = anthropic.Anthropic(api_key=ANTHROPIC_API_KEY)

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

def governed_claude_message(model: str, system: str, messages: list, tools: list = None) -> anthropic.types.Message:
    """
    A governed wrapper around an Anthropic Claude message call.
    1. Authorize the prompt with Sentinel.
    2. Call the Claude API.
    3. Record the result with Sentinel.
    """
    correlation_id = str(uuid.uuid4())
    
    # We combine system and messages to create a comprehensive prompt hash representation
    prompt_text = json.dumps({"system": system, "messages": messages, "tools": tools or []})

    # Step 1: authorize
    decision = sentinel_ai_authorize(model, prompt_text, correlation_id)
    if decision.get("decision") == "deny":
        raise PermissionError(f"AI call denied by Sentinel policy: {decision.get('decision_id')}")

    packet_id = decision.get("packet_id")

    # Step 2: call upstream model
    kwargs = {
        "model": model,
        "max_tokens": 1024,
        "system": system,
        "messages": messages,
    }
    if tools:
        kwargs["tools"] = tools

    response = client.messages.create(**kwargs)

    # Step 3: record result
    # We figure out how many tool calls were made in the response
    tool_call_count = sum(1 for block in response.content if block.type == "tool_use")
    response_text = " ".join([block.text for block in response.content if block.type == "text"])
    
    sentinel_ai_result(packet_id, response_text, tool_call_count, correlation_id)

    return response

if __name__ == "__main__":
    if not ANTHROPIC_API_KEY:
        print("Please set ANTHROPIC_API_KEY to run the API call against Claude.")
    else:
        print("Sending governed request to Claude...")
        result = governed_claude_message(
            model="claude-3-7-sonnet-20250219",
            system="You are a helpful billing assistant. Be concise.",
            messages=[{"role": "user", "content": "Can you summarize our refund policy?"}],
        )
        print("--- Claude Response ---")
        print(result.content[0].text)
