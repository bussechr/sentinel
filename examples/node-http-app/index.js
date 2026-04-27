// Node.js / Express example: Sentinel REST integration
// Demonstrates POST /v1/packets/authorize before a sensitive operation.
//
// Install: npm install express node-fetch
// Run:
//   SENTINEL_API=http://localhost:8080 node index.js

const express = require("express");
const fetch = require("node-fetch").default ?? require("node-fetch");
const crypto = require("crypto");

const app = express();
app.use(express.json());

const SENTINEL_API = process.env.SENTINEL_API || "http://localhost:8080";
const APP_ID = "node-billing-api";

/**
 * Authorize an action with Sentinel before executing it.
 * Returns the decision object or throws on network failure (fail-open in observe).
 */
async function sentinelAuthorize({ actionName, category, risk, mutating, payloadBody, correlationId }) {
  const bodyHash = "sha256:" + crypto.createHash("sha256").update(JSON.stringify(payloadBody || {})).digest("hex");

  const payload = {
    app_id: APP_ID,
    actor_type: "service",
    action_name: actionName,
    category,
    risk,
    mutating,
    payload_hash: bodyHash,
    correlation_id: correlationId || crypto.randomUUID(),
  };

  try {
    const resp = await fetch(`${SENTINEL_API}/v1/packets/authorize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      signal: AbortSignal.timeout(3000),
    });
    return await resp.json();
  } catch (err) {
    // Fail-open: Sentinel unavailable → allow but log degraded state.
    console.warn("[sentinel] degraded:", err.message);
    return { decision: "allow", decision_id: "degraded", packet_id: null };
  }
}

// POST /refund — high-risk mutating action.
app.post("/refund", async (req, res) => {
  const correlationId = req.headers["x-correlation-id"] || crypto.randomUUID();

  const decision = await sentinelAuthorize({
    actionName: "invoice.refund.create",
    category: "http",
    risk: "high",
    mutating: true,
    payloadBody: req.body,
    correlationId,
  });

  if (decision.decision === "deny") {
    return res.status(403).json({
      error: "denied by sentinel policy",
      decision_id: decision.decision_id,
    });
  }

  // Proceed with the refund logic here.
  res.set("x-correlation-id", correlationId);
  res.set("x-sentinel-decision", decision.decision);
  res.status(202).json({ status: "refund accepted", packet_id: decision.packet_id });
});

// GET /status — low-risk read.
app.get("/status", async (req, res) => {
  await sentinelAuthorize({ actionName: "billing.status.read", category: "http", risk: "low", mutating: false });
  res.json({ status: "ok" });
});

const PORT = process.env.PORT || 9091;
app.listen(PORT, () => console.log(`node-billing-api listening on :${PORT} (sentinel at ${SENTINEL_API})`));
