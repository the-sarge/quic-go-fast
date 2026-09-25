# Approved experiment ceiling: 56 hours

Status: operator approved **56 experiment-hours** on 2026-09-25, replying
“Approve 56 experiment-hours.” Preparation remains 16 hours and cloud cost $100.
The controller guard must be updated before relying on the extended allowance.
No additional scenario, seed, sensitivity, or controller comparison is proposed.

The current controller ledger retains 6.718 experiment-hours and $11.845 of
reservations, including failed starts and conservative cleanup allowances. These
are reservations, not settled billing or measured workload time.

| Remaining or retained work | Conservative hours |
| --- | ---: |
| Existing reservations | 6.718 |
| Accepted core warm-up and measurement (460 cases) | 36 |
| Accepted completion allocation, including its setup allowance | 2 |
| Remaining Linux, Windows and Mac validation allocations | 2.5 |
| Core case launch/export and topology teardown | 6 |
| Forecast before contingency | 53.218 |
| Contingency under the approved 56-hour ceiling | 2.782 |

The six-hour command allowance is a planning bound, not an observed campaign
runtime. The current commands use a 25-second future start. Across 460 core
cases that alone adds 3.194 hours; the gateway now stays alive for 20 seconds after measurement to cover the
fixture’s bounded final receipt. Across 460 core cases that tail adds up to
2.556 hours, making a six-hour command allowance necessary. Topology turnover
and any excess still consume the remaining contingency. Reducing that allowance requires validating a faster command
sequence, not assuming zero setup cost. Completion setup is already inside its
two-hour allocation and is not charged again in this core allowance.

The 2.5-hour remaining validation allocation comprises one Linux topology hour,
half an hour for Windows with the selected gateway settings, and one hour for
the local Mac path after native path qualification under the approved Mac scheduling exception. These are
finite planning allocations, not authorization to run on an unqualified path.
Any launch still needs fresh quota/price evidence and a frozen expiry/reservation.

The $100 cloud ceiling need not increase. Using $1.90/hour even for all Linux
core hours with the optional competitor pair retained gives about $60.17 for
Linux and Windows core VM time. Existing reservations, $3.35 for the proposed
remaining cloud validation, $3.80 for completions, and $11.40 for six hours of
cloud command overhead total about $90.57 before contingency and new export/
storage reserves. The remaining $9.43 must cover those items; they are not free.
Local Mac time has no GCP VM charge. Current prices must still be refreshed
before subsequent paid launches, and both numerical ceilings remain enforced.

Approval changes only the overall experiment ceiling to 56 hours. Preserve
all existing reservations and invalid observations; keep the accepted 520-case
inventory, $100 cloud limit and 16-hour preparation limit. The Mac scheduling exception was separately approved on the same date. The
new M4 mini–minimax cable is detected by both hosts at a reported 80 Gb/s; this
is link inventory, not a measured throughput or gateway calibration result.
