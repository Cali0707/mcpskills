---
name: engineering-rollout
description: Prepare an engineering-only, reversible rollout checklist for the fictional Orbit 1.4 release.
metadata:
  audience: engineering
  scenario: Orbit 1.4 launch
---

# Engineering rollout plan

Use this skill to draft a rollout plan or assess whether a fictional Orbit 1.4
release is ready. It is a separate nested skill: being allowed to read the
parent `launch-brief` does not grant access here.

1. Read [the rollout checks](runbooks/rollout-checks.md).
2. Present preflight, staged rollout, observation, and rollback steps.
3. State what evidence would trigger a pause or rollback.
4. Do not claim a deployment occurred or run commands; this is a planning demo.

Example prompt: "Draft the engineering rollout checklist for Orbit 1.4."
