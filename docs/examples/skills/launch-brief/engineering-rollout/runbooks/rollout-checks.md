# Fictional Orbit 1.4 rollout checks

| Phase | Example check | Pause or rollback signal |
| --- | --- | --- |
| Preflight | Verify export jobs in the staging demo dataset | Missing or duplicate weekly summaries |
| Stage 1 | Enable saved views for the first demo team | Views cannot be opened after saving |
| Stage 2 | Enable remaining two demo teams | Error rate above 2% for ten minutes |
| Rollback | Disable the release flag and confirm old views still load | Errors persist after the flag is disabled |

These are illustrative thresholds for a fictional system, not live operational
instructions. Report observations separately from proposed actions.
