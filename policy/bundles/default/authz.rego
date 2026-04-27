# Default Sentinel OPA policy bundle
# Policy path: data.sentinel.authz
#
# This bundle provides three modes: observe, guard, enforce.
# The result is a map: { decision, reason }
#
# Load this bundle into OPA with:
#   opa run --server --bundle ./policy/bundles/default/

package sentinel.authz

import future.keywords.if
import future.keywords.in

# ─── Top-level decision ───────────────────────────────────────────────────────

default result := {"decision": "allow", "reason": "default allow"}

result := {"decision": "deny", "reason": reason} if {
    some reason
    deny_reasons[reason]
}

result := {"decision": "warn", "reason": reason} if {
    count(deny_reasons) == 0
    some reason
    warn_reasons[reason]
}

# ─── Deny rules ───────────────────────────────────────────────────────────────

deny_reasons[reason] if {
    not input.app.app_id
    reason := "unknown application: app_id is required"
}

deny_reasons[reason] if {
    not registered_apps[input.app.app_id]
    reason := sprintf("unregistered application: %v", [input.app.app_id])
}

deny_reasons[reason] if {
    input.action.risk in {"high", "critical"}
    input.mode == "enforce"
    not input.actor.id_hash
    reason := "enforce mode: actor identity required for high/critical risk"
}

deny_reasons[reason] if {
    input.ai.is_ai_related
    not input.ai.prompt_hash
    reason := "AI packet missing prompt_hash"
}

# ─── Warn rules ───────────────────────────────────────────────────────────────

warn_reasons[reason] if {
    input.action.risk in {"high", "critical"}
    input.mode == "observe"
    reason := sprintf("observe mode: high-risk action %v is advisory only", [input.action.name])
}

warn_reasons[reason] if {
    input.action.mutating
    input.action.risk == "medium"
    reason := sprintf("mutating medium-risk action: %v", [input.action.name])
}

# ─── Registered apps (loaded from data bundle) ───────────────────────────────
# In production, populate data.sentinel.registered_apps from the app registry.
# This stub allows any app_id during development.

registered_apps[app_id] if {
    app_id := input.app.app_id
    # TODO: replace with data.sentinel.registered_apps lookup in M1.
}
