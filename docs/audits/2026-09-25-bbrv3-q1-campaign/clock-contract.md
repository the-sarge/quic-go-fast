# Clock and event alignment contract

Durations, completion latency and RTT use one process's local clock. Keep their
original denominators. Completion uses a monotonic elapsed interval; control RTT
and scheduled byte bins use local wall-clock timestamps and require no clock step.
The first observer receipts describe all local durations as monotonic; that
metadata phrase is too broad and is superseded by this distinction. Raw wall-clock timestamps from different hosts must not
be subtracted or put on one event axis without their clock observations.

The finite `clock-observe.py` preparation aid exchanges ten requests over one
persistent management connection per host and makes no clock adjustment.
For controller send/receive times C0/C1 and the enclosed remote sample R,
remote-minus-controller offset is in [R-C1, R-C0]. The intersection is valid
for that short observation; an empty intersection is a failed clock check.
For offset interval [L,U], a remote timestamp T maps to controller interval
[T-U,T-L]. Preserve that interval rather than treating its midpoint as exact.
The source and raw samples are retained with the native receipts.

The first local observation bounded MBP-minus-M4 at 29.006–29.407 ms and
minimax-minus-M4 at 30.783–31.073 ms. These are real schedule offsets, not
network propagation. A shared numeric start value does not eliminate them.
Translate gateway event times into the receiver's clock domain before aligning
phase plots or per-second receiver bins, and disclose uncertainty at boundaries.
The initial observation does not certify a later idle measurement window.

Cloud IAP round trips produce wider management-channel bounds. Also retain
`chronyc tracking` on every Linux participant: selected source, leap state,
remaining system correction, root delay and dispersion. With a correct upstream
time source, chrony gives a clock-error estimate from the absolute remaining
system correction plus root dispersion plus half the root delay; add endpoint
and gateway errors when comparing their timestamps. This is conditional on the
time source and the recorded observation, not a hardware timestamp guarantee.
See [chrony's tracking documentation](https://chrony.gitlab.io/doc/4.9/chronyc.html)
and [accuracy FAQ](https://chrony-project.org/faq.html).

For Windows retain native `w32tm` source/status together with management-channel
samples. Never infer Windows accuracy from the Linux gateway's NTP state. The
persistent stdin observer timed out on native Windows before L3 measurement.
Its replacement takes ten independent timestamp observations per host; the
launch/completion bounds include process and IAP setup and are much wider.
The final Windows preparation observation uses one independent sample per host
and retains `w32tm`/chrony status. Neither method establishes sub-millisecond
Windows synchronization; retain the raw intervals and avoid precise cross-host
latency claims based on them.

The fixture also retains a `clock_offset_bounds_ns` interval for each completed
control exchange throughout warm-up and measurement. These bound the peer
clock using that exchange; retain the wider bounds when modeled queues add delay.
They complement gateway clock observations and must not be treated as point
estimates of one-way network delay.

Before a campaign lease, repeat the native clock observations after startup and
retain observations at its end. Record any clock step, changed source or inconsistent
bound as an alignment failure. Do not interpolate across such a discontinuity
or silently repair an observation. The raw configs and actual phase/pause times
remain authoritative. Report the observed uncertainty and any unobserved drift
limitation with cross-host timing; use receiver-local byte totals and completion
measurements for claims that do not need synchronized clocks.
