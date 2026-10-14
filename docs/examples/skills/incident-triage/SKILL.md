---
name: incident-triage
description: Triage a fictional Orbit 1.4 export incident and draft an engineering handoff.
metadata:
  audience: engineering
  scenario: Orbit 1.4 launch
---

# Orbit export incident triage

Use this skill when an engineering demo user asks about a failed weekly summary
export. Read [the incident signals](references/incident-signals.md) first.

1. Separate known facts from hypotheses and unknowns.
2. State the potential user impact and what evidence is needed next.
3. Propose a reversible mitigation, a verification step, and an owner for follow-up.
4. Produce a short handoff: summary, impact, evidence, next action, and status.
5. Do not claim to have checked logs or changed a running system.

Example prompt: "Triage an Orbit 1.4 weekly export failure."
