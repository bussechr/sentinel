// Canonical Sentinel packet & decision types — kept in lockstep with
// internal/core/packet.go. Update both sides together.

export type Mode = "observe" | "guard" | "enforce";

export type ActorType = "human" | "service" | "agent" | "model" | "system";

export type ActionCategory =
  | "http"
  | "grpc"
  | "ai"
  | "tool"
  | "db"
  | "file"
  | "network"
  | "k8s"
  | "secret"
  | "config";

export type RiskLevel = "low" | "medium" | "high" | "critical";

export type Decision = "allow" | "warn" | "deny" | "escalate";

export interface RoutePolicy {
  actionName: string;
  category: ActionCategory;
  risk: RiskLevel;
  mutating?: boolean;
  resourceType?: string;
}

export interface AuthorizeRequest {
  app_id: string;
  actor_type: ActorType;
  action_name: string;
  category: ActionCategory;
  risk: RiskLevel;
  mutating: boolean;
  resource_type?: string;
  payload_hash: string;
  correlation_id?: string;
  trace_id?: string;
  actor_id_hash?: string;
}

export interface AuthorizeResponse {
  decision: Decision;
  decision_id: string;
  packet_id: string;
  reason?: string;
  ledger_required?: boolean;
  receipt_id?: string;
  receipt_status?: string;
  witness_id?: string;
}

export interface AIAuthorizeRequest {
  app_id: string;
  correlation_id: string;
  model_id_hash: string;
  prompt_hash: string;
  tool_call_count?: number;
}

export interface AIAuthorizeResponse {
  decision: Decision;
  decision_id: string;
  packet_id: string;
  reason?: string;
}

export interface AIResultRequest {
  app_id: string;
  correlation_id: string;
  packet_id?: string;
  response_hash: string;
  tool_call_count: number;
}

export interface CausalGraph {
  correlation_id: string;
  graph: {
    correlation_id: string;
    nodes: { id: string; kind: string; at: string; label: string }[];
    edges: { from: string; to: string; kind: string }[];
  };
  anchored: boolean;
  first_deny?: { id: string; at: string };
}

export interface WriterHealth {
  kind: string;
  name: string;
  healthy: boolean;
  height: number;
  reason?: string;
}

export interface WriterHealthList {
  writers: WriterHealth[];
  default: string;
}

export interface ShadowDivergencesResponse {
  since: string;
  rows: {
    shadow_id: string;
    packet_id: string;
    correlation_id: string;
    active_bundle_id: string;
    candidate_bundle_id: string;
    active_decision: Decision;
    candidate_decision: Decision;
    diverged: boolean;
    active_reason?: string;
    candidate_reason?: string;
    evaluated_at: string;
  }[];
}
