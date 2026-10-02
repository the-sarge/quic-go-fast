# Commit matrix

The final `bbr_conformance_test.go` (at the full candidate) run against each code revision, each in a clean `git archive` extract with `GOWORK=off`.

| Subtest | `c6bb13b7` C1–C3 base | `fd0b90b2` +item 4 | `1710d64f` +item 2 | `f3383912` +item 3 | `558d1729` +item 1 | `8a0e0962` +Up slope fix |
| --- | --- | --- | --- | --- | --- | --- |
| `TestBBRUndoRestoresLossDrivenPhase/Startup_all-spurious` | FAIL | FAIL | FAIL | FAIL | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/Up_all-spurious` | FAIL | FAIL | FAIL | FAIL | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/Startup_mixed_control` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/Up_mixed_control` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/control:_plateau_exit_is_not_loss-driven` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/control:_Cruise_undo_keeps_Cruise` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/ProbeRTT_keeps_measuring,_then_returns_to_Startup` | FAIL | FAIL | FAIL | FAIL | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/control:_mixed_ProbeRTT_exit_returns_to_Cruise` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/control:_Up_undo_during_ProbeRTT_keeps_measuring,_then_Cruise` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/control:_CE_cap_blocks_Startup_restoration` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRUndoRestoresLossDrivenPhase/control:_CE_cap_blocks_Refill_restoration` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/Drain_lower_RTT` | FAIL | FAIL | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/Drain_rejected_rate_lower_RTT` | FAIL | FAIL | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/ProbeBW_Down_lower_RTT` | FAIL | FAIL | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/Drain_control:_unchanged_RTT` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/Drain_rejected_rate_control:_unchanged_RTT` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/ProbeBW_Down_control:_unchanged_RTT` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/Startup_loss_lower_RTT` | FAIL | FAIL | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/control:_Startup_loss_unchanged_RTT` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/control:_a_stale-delivered_ACK_still_updates_the_minimum` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/Drain_expired_increasing_minimum` | FAIL | FAIL | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/control:_same-ACK_ProbeRTT_entry,_lower_RTT` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/control:_same-ACK_ProbeRTT_entry,_expired_higher_minimum` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPhaseDecisionsUsePreUpdateMinRTT/control:_same-ACK_ProbeRTT_entry,_rejected_rate` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Drain_rejected_rate` | FAIL | FAIL | FAIL | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Refill_rejected_rate` | FAIL | FAIL | FAIL | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Drain_rejected_interval` | FAIL | FAIL | FAIL | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Refill_rejected_interval` | FAIL | FAIL | FAIL | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Drain_control:_valid_rate` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Refill_control:_valid_rate` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Drain_control:_missing_history` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Refill_control:_missing_history` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Drain_control:_absent_anchor` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Refill_control:_absent_anchor` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Drain_control:_invalid_clock` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/Refill_control:_invalid_clock` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/control:_valid_Up_round_raises_the_slope_and_grows_the_bound` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/rejected_Up_round_raises_the_slope,_not_the_bound` | FAIL | FAIL | FAIL | FAIL | FAIL | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/rejected_rate_leaves_the_safe-flight_bound_raise` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRPacketRoundsIgnoreRateValidity/rejected_rate_leaves_rate-owned_model_state` | FAIL | FAIL | FAIL | PASS | PASS | PASS |
| `TestBBRStartupLossLearnsUnquantizedCapacity/two_quanta_exceed_BDP` | FAIL | PASS | PASS | PASS | PASS | PASS |
| `TestBBRStartupLossLearnsUnquantizedCapacity/output_floor_masks_the_difference` | FAIL | PASS | PASS | PASS | PASS | PASS |
| `TestBBRStartupLossLearnsUnquantizedCapacity/control:_high_BDP` | PASS | PASS | PASS | PASS | PASS | PASS |
| `TestBBRStartupLossLearnsUnquantizedCapacity/control:_latest_volume_above_BDP` | PASS | PASS | PASS | PASS | PASS | PASS |

Pre-existing `internal/congestion` tests (conformance tests skipped) at each revision:

- c6bb13b7 ok  	github.com/quic-go/quic-go/internal/congestion	0.118s
- fd0b90b2 ok  	github.com/quic-go/quic-go/internal/congestion	0.111s
- 1710d64f ok  	github.com/quic-go/quic-go/internal/congestion	0.119s
- f3383912 ok  	github.com/quic-go/quic-go/internal/congestion	0.117s
- 558d1729 ok  	github.com/quic-go/quic-go/internal/congestion	0.126s
- 8a0e0962 ok  	github.com/quic-go/quic-go/internal/congestion	0.130s
