# Extra CHECK experiment

Local Docker demo, 2026-10-20. Five teams, one notes service per team,
60-second classic rounds, 10-second checker timeout, `puts = gets = 2`,
`extra_checks = 2`. Existing team identities, tokens, task settings, flag
lifetime and session generation were retained. A database/configuration backup
was saved before the schema change. The experiment preceded consolidation into
the single initial migration.

## Results

| Scenario | Observation |
| --- | --- |
| All services available, rounds 70-72 | 45/45 jobs UP; 15 successful team/round observations. |
| daywave periodically unavailable, rounds 74-78 | Main workflow failed in 3/5 rounds; the combined result failed in 5/5. |
| Four control teams during outages | All 20 team/round observations passed; no false DOWN. |
| Pause and ticker/worker restart | All 10 pending CHECK IDs and times survived restart. Resume shifted their times by 21.633 seconds; all 10 completed UP. |
| Queue completion | No missing or expired jobs in the availability samples. Maximum publication delay after `not_before`: 110 ms healthy, 118 ms with faults. Maximum sent-to-completion interval: 3.36 seconds. |

The fault test used an independent fixed cycle: 15 seconds available, then
22 seconds rejecting connections to team 3's notes port. It did not consult
checker timing. The temporary nftables table was removed and its absence
verified. The other services stayed available.

The 3/5 and 5/5 figures compare results within the same experimental rounds.
They are not separate randomized trials of configurations 0 and 2. In rounds
76 and 78 the main workflow passed, while an extra CHECK detected the outage.
The healthy and fault runs contain 120 jobs in total.

## Validation

- Go unit/race tests and the complete isolated integration suite passed.
- Focused integration checks passed again after the final changes: durable
  scheduling, concurrent completion, stale-worker fencing, admin pause/resume,
  queue expiry and task timeout validation.
- Go lint, SQL lint, Ruff and infrastructure/demo Python tests passed.
- A CHECK-only worker test verifies that neither flag insertion nor retrieval
  is called. Normal PUT/GET scheduling and flag lifetime remain unchanged.
- After consolidation, the full integration suite with `-race`, Go lint and SQL
  lint passed. Tests cover the single initial migration, one-step rollback and
  reapply, preservation of extra-CHECK state and the prior experimental ledger,
  and refusal of missing-column schemas without changing data.

Run the three commands in [README.md](README.md#availability-experiment) to
repeat the experiment. Private raw reports are written to
`config/runtime/demo/availability-{healthy,fault,pause}.json`.

## Decision

Keep the general default at `0` and demo at `2`. The extra checks detected two
outages missed by the main workflow in this sample, with no false DOWN in the
control group. This supports the feature as an opt-in experiment.

This small run does not prove continuous availability, resistance to every
checker-detection strategy, or production capacity. It tests roughly 41%
availability during the fault cycle, not the original claimed 2% scenario.
Two samples are not established as sufficient for an event.

Job count rises from 5 to 15 per round in this demo. For a fully successful
workflow with two PUTs and two GETs, checker actions rise from 5 to 7 per
team/service; CPU and memory costs depend on the real checker. Test real
checkers, longer outage sequences and the intended team/service count before
enabling this by default.
