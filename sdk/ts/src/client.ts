// Sentinel TypeScript SDK.
//
// The SDK exposes:
//   - authorize()           : POST /v1/packets/authorize
//   - aiAuthorize()         : POST /v1/ai/authorize (machine-actor lane)
//   - aiResult()            : POST /v1/ai/result
//   - rewind()              : GET  /v1/evidence/rewind/{correlationID}
//   - causalGraph()         : GET  /v1/evidence/causal/{correlationID}
//   - listWriters()         : GET  /v1/ledger/writers
//   - shadowDivergences()   : GET  /v1/policy/shadow/divergences

import { sha256Hex } from "./hash.js";
import type {
  AIAuthorizeRequest,
  AIAuthorizeResponse,
  AIResultRequest,
  AuthorizeRequest,
  AuthorizeResponse,
  CausalGraph,
  Mode,
  RoutePolicy,
  ShadowDivergencesResponse,
  WriterHealthList,
} from "./types.js";

export const HEADER_AI_ROUTE = "X-Sentinel-AI-Route";
export const HEADER_AI_ACTOR = "X-Sentinel-AI-Actor";
export const AI_ROUTE_VALUE = "ai-gateway";

export interface ClientConfig {
  endpoint: string;
  appId: string;
  mode: Mode;
  /** Optional bearer token; production should rotate via SPIFFE SVID. */
  token?: string;
  /** Per-request timeout in milliseconds. Default 5000. */
  timeoutMs?: number;
  /** Optional fetch implementation override (for tests / non-standard runtimes). */
  fetchImpl?: typeof globalThis.fetch;
}

export class SentinelClient {
  private readonly cfg: ClientConfig;
  private readonly fetcher: typeof globalThis.fetch;

  constructor(cfg: ClientConfig) {
    this.cfg = { timeoutMs: 5000, ...cfg };
    this.fetcher = cfg.fetchImpl ?? fetch;
  }

  /** Sentinel /v1/packets/authorize. */
  async authorize(
    correlationId: string,
    policy: RoutePolicy,
    payloadHash: string,
    extra: Partial<AuthorizeRequest> = {}
  ): Promise<AuthorizeResponse> {
    const body: AuthorizeRequest = {
      app_id: this.cfg.appId,
      actor_type: extra.actor_type ?? "service",
      action_name: policy.actionName,
      category: policy.category,
      risk: policy.risk,
      mutating: !!policy.mutating,
      resource_type: policy.resourceType,
      payload_hash: payloadHash,
      correlation_id: correlationId,
      trace_id: extra.trace_id,
      actor_id_hash: extra.actor_id_hash,
    };

    try {
      return await this.post<AuthorizeResponse>("/v1/packets/authorize", body);
    } catch (err) {
      if (this.cfg.mode === "observe") {
        return {
          decision: "allow",
          decision_id: "degraded",
          packet_id: "",
          reason: `sentinel degraded: ${(err as Error).message}`,
        };
      }
      throw err;
    }
  }

  /** Sentinel /v1/ai/authorize. Adds the AI gateway headers automatically. */
  async aiAuthorize(req: AIAuthorizeRequest): Promise<AIAuthorizeResponse> {
    return this.post<AIAuthorizeResponse>("/v1/ai/authorize", req, {
      [HEADER_AI_ROUTE]: AI_ROUTE_VALUE,
      [HEADER_AI_ACTOR]: "model",
    });
  }

  /** Sentinel /v1/ai/result. */
  async aiResult(req: AIResultRequest): Promise<{ status: string }> {
    return this.post<{ status: string }>("/v1/ai/result", req, {
      [HEADER_AI_ROUTE]: AI_ROUTE_VALUE,
      [HEADER_AI_ACTOR]: "model",
    });
  }

  /** Sentinel /v1/evidence/rewind/{correlationID}. */
  async rewind(correlationId: string, windowDuration?: string): Promise<unknown> {
    const path =
      `/v1/evidence/rewind/${encodeURIComponent(correlationId)}` +
      (windowDuration ? `?window=${encodeURIComponent(windowDuration)}` : "");
    return this.get<unknown>(path);
  }

  /** Sentinel /v1/evidence/causal/{correlationID}. */
  async causalGraph(correlationId: string): Promise<CausalGraph> {
    return this.get<CausalGraph>(
      `/v1/evidence/causal/${encodeURIComponent(correlationId)}`
    );
  }

  /** Sentinel /v1/ledger/writers — multi-backend health snapshot. */
  async listWriters(): Promise<WriterHealthList> {
    return this.get<WriterHealthList>("/v1/ledger/writers");
  }

  /** Sentinel /v1/policy/shadow/divergences. */
  async shadowDivergences(
    since?: Date,
    limit = 50
  ): Promise<ShadowDivergencesResponse> {
    const params = new URLSearchParams();
    if (since) params.set("since", since.toISOString());
    if (limit) params.set("limit", String(limit));
    const q = params.toString();
    return this.get<ShadowDivergencesResponse>(
      `/v1/policy/shadow/divergences${q ? `?${q}` : ""}`
    );
  }

  /** Convenience: compute the canonical body hash for a payload. */
  hash(value: string | Uint8Array): Promise<string> {
    return sha256Hex(value);
  }

  // ────────────────── internals ──────────────────

  private async post<T>(
    path: string,
    body: unknown,
    extraHeaders: Record<string, string> = {}
  ): Promise<T> {
    return this.request<T>(path, "POST", body, extraHeaders);
  }

  private async get<T>(path: string): Promise<T> {
    return this.request<T>(path, "GET");
  }

  private async request<T>(
    path: string,
    method: "GET" | "POST",
    body?: unknown,
    extraHeaders: Record<string, string> = {}
  ): Promise<T> {
    const url = this.cfg.endpoint.replace(/\/+$/, "") + path;
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), this.cfg.timeoutMs ?? 5000);

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...extraHeaders,
    };
    if (this.cfg.token) headers["Authorization"] = `Bearer ${this.cfg.token}`;

    try {
      const resp = await this.fetcher(url, {
        method,
        headers,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal: ctrl.signal,
      });
      const text = await resp.text();
      if (!resp.ok) {
        throw new Error(
          `sentinel: ${method} ${path} -> ${resp.status}: ${text || resp.statusText}`
        );
      }
      return text ? (JSON.parse(text) as T) : (undefined as T);
    } finally {
      clearTimeout(t);
    }
  }
}

/** HTTP middleware that wraps an Express/Connect-style handler. */
export function withSentinel(
  client: SentinelClient,
  policy: RoutePolicy
): (req: AnyReq, res: AnyRes, next: () => void) => Promise<void> {
  return async (req, res, next) => {
    const correlationId =
      (req.headers && (req.headers["x-correlation-id"] as string)) ||
      `corr_${cryptoRandomId()}`;
    const bodyText =
      typeof req.body === "string"
        ? req.body
        : req.body
        ? JSON.stringify(req.body)
        : "";
    const payloadHash = await sha256Hex(bodyText);
    try {
      const decision = await client.authorize(correlationId, policy, payloadHash);
      if (res.setHeader) {
        res.setHeader("X-Correlation-ID", correlationId);
        res.setHeader("X-Sentinel-Decision", decision.decision);
        res.setHeader("X-Sentinel-Packet-ID", decision.packet_id);
      }
      if (decision.decision === "deny") {
        res.statusCode = 403;
        res.end?.(
          JSON.stringify({ error: "denied by sentinel policy", decision })
        );
        return;
      }
      next();
    } catch (err) {
      if (res.setHeader) {
        res.setHeader("X-Correlation-ID", correlationId);
      }
      // observe mode short-circuits inside Authorize already; if it threw,
      // we are in guard/enforce mode and must block.
      res.statusCode = 503;
      res.end?.(
        JSON.stringify({ error: `sentinel unavailable: ${(err as Error).message}` })
      );
    }
  };
}

interface AnyReq {
  headers?: Record<string, string | string[] | undefined>;
  body?: unknown;
}
interface AnyRes {
  statusCode?: number;
  setHeader?: (k: string, v: string) => void;
  end?: (body?: string) => void;
}

function cryptoRandomId(): string {
  if (globalThis.crypto && "randomUUID" in globalThis.crypto) {
    return globalThis.crypto.randomUUID();
  }
  // RFC4122-ish fallback.
  const buf = new Uint8Array(16);
  for (let i = 0; i < buf.length; i++) buf[i] = Math.floor(Math.random() * 256);
  return Array.from(buf, (b) => b.toString(16).padStart(2, "0")).join("");
}

