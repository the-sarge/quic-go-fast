# Approved experiment ceiling: 56 hours

Status: operator approved **56 experiment-hours** on 2026-09-25, replying
“Approve 56 experiment-hours.” Preparation remains 16 hours and cloud cost $100.
The controller guard must be updated before relying on the extended allowance.
No additional scenario, seed, sensitivity, or controller comparison is proposed.

The current controller ledger retains 8.718 experiment-hours and $13.995 of
reservations, including failed starts and conservative cleanup allowances. These
are reservations, not settled billing or measured workload time.

| Remaining or retained work | Conservative hours |
| --- | ---: |
| Existing reservations | 8.718 |
| Accepted core warm-up and measurement (460 cases) | 36 |
| Accepted completion allocation, including its setup allowance | 2 |
| Remaining Linux, Windows and Mac validation allocations | 1.75 |
| Core case launch/export and topology teardown | 6 |
| Forecast before contingency | 54.468 |
| Contingency under the approved 56-hour ceiling | 1.532 |

The six-hour command allowance is a planning bound, not an observed campaign
runtime. The current commands use a 25-second future start. Across 460 core
cases that alone adds 3.194 hours; the gateway now stays alive for 20 seconds after measurement to cover the
fixture’s bounded final receipt. Across 460 core cases that tail adds up to
2.556 hours, making a six-hour command allowance necessary. Topology turnover
and any excess still consume the remaining contingency. Reducing that allowance requires validating a faster command
sequence, not assuming zero setup cost. Completion setup is already inside its
two-hour allocation and is not charged again in this core allowance.

The remaining 1.75-hour validation allocation comprises one Linux topology
hour with the explicit four-core gateway candidate, half an hour for Windows,
and 15 minutes for the outstanding Mac reverse-capacity diagnostic. The prior
one-hour Mac window completed and automatically expired; a new temporary setup
and idle window are required for that last diagnostic. Failed observations and
unused portions of earlier reservations remain charged. These are finite
planning allocations, not claims of qualified paths. Any cloud launch still
needs fresh quota/price evidence and a frozen expiry/reservation.

The four-core candidate costs $1.30/hour for the three-VM Linux topology,
$2.10/hour for Linux with competitors, and $2.10/hour for Windows without
competitors. Keep competitor VMs only for Linux L4/L6/L7/L8: those rows consume
eight core hours; other Linux rows consume 19 hours 20 minutes. Windows consumes
4 hours 20 minutes. This yields $51.03 of core cloud time. The lease manifest
must preserve controller pairs on the same hosts and explicitly account for
bootstrap, teardown and rounded lease reservations; this arithmetic alone is
not a finished lease manifest.

Retained reservations $13.995, the proposed Linux validation $2.35, Windows
validation $1.30, core cloud time $51.03, completion allowance $4.20, and six
hours of cloud command overhead $12.60 total approximately **$85.48**. The
remaining **$14.52** covers storage/export, topology turnover, reservation
rounding and contingency under $100. Mac validation has no GCP VM charge.
This forecast is conditional on the described competitor lease partition;
keeping competitors for every Linux row would invalidate it. Published price
observations remain subject to the controller's 24-hour freshness gate.

Approval changes only the overall experiment ceiling to 56 hours. Preserve
all existing reservations and invalid observations; keep the accepted 520-case
inventory, $100 cloud limit and 16-hour preparation limit. The Mac scheduling exception was separately approved on the same date. The
new M4 mini–minimax cable is detected by both hosts at a reported 80 Gb/s; this
is link inventory, not a measured throughput or gateway calibration result.
