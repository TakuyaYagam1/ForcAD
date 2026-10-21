# Extra CHECK experiment

Local Docker demo run on 2026-10-20. It used five teams, one notes service per
team, 60-second classic rounds, a 10-second checker timeout, two PUTs, two GETs,
and `extra_checks = 2`. The run kept team identities, tokens, task settings,
flag lifetime, and session generation. A database and config backup was saved
before the schema change.

## Results

| Scenario | Result |
| --- | --- |
| Healthy services, rounds 70-72 | All 45 checker jobs completed UP across 15 team-rounds. |
| Fault on daywave, rounds 74-78 | The main workflow failed in 3/5 rounds; the combined result failed in 5/5. |
| Four control teams during the fault | All 20 team-rounds passed, with no false DOWN. |
| Pause and ticker/worker restart | All 10 pending CHECK IDs and times survived. Resume shifted their times by 21.633 seconds; all 10 completed UP. |
| Queue completion | No missing or expired jobs. Maximum queue delay after `not_before` was 110 ms when healthy and 118 ms with faults. Maximum sent-to-completion time was 3.36 seconds. |

The fault test used a fixed wall-clock cycle: 15 seconds available, then 22
seconds rejecting connections to team 3's notes port. It did not read checker
timing. The other services stayed available. The temporary nftables table was
removed and its absence verified.

The main and combined results compare the same five fault rounds. They are not
randomized trials of `extra_checks = 0` and `2`. In rounds 76 and 78, the main
workflow passed while an extra CHECK detected the outage. The healthy and fault
runs contain 120 jobs in total.

## Validation

- Go unit and race tests, the isolated integration suite, Go lint, SQL lint,
  Ruff, and infrastructure/demo Python tests passed.
- Focused integration checks after the final changes covered durable scheduling,
  concurrent completion, stale-worker fencing, admin pause/resume, queue expiry,
  and task timeout validation.
- A CHECK-only worker test confirms that extra checks do not insert or retrieve
  flags. Normal PUT/GET scheduling and flag lifetime remain unchanged.
- After migration consolidation, the full integration suite with `-race`, Go
  lint, and SQL lint passed. Tests cover the initial migration, rollback and
  reapply, preservation of extra-CHECK state and the earlier experiment ledger,
  and refusal of a missing-column schema without modifying data.

Run the commands in [the demo README](README.md#availability-experiment) to
repeat the experiment. Private reports are written to
`config/runtime/demo/availability-{healthy,fault,pause}.json`.

## Decision

Keep the general default at `0` and the demo at `2`. Extra checks caught two
outages that the main workflow missed in this sample, with no false DOWN in the
control teams. This supports the feature as an opt-in experiment.

The fault cycle kept the target available for about 41% of the time. This run
does not represent a 2%-availability cycle. It also does not prove continuous
availability, resistance to every checker-detection strategy, or production
capacity. This sample does not establish that two extra checks are enough for
an event.

The demo creates 15 jobs per round instead of 5. For a fully successful
workflow with two PUTs and two GETs, each team/task goes from 5 checker actions
to 7. The real CPU and memory costs depend on the checker. Test real checkers
with longer outages and the intended team and service count before considering
a nonzero default.
