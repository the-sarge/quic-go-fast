# Triage labels

Map each canonical triage role to the following GitHub label.

| Canonical role | Repository label | Meaning |
| --- | --- | --- |
| `needs-triage` | `needs-triage` | Maintainer needs to evaluate |
| `needs-info` | `needs-info` | Waiting on reporter |
| `ready-for-agent` | `ready-for-agent` | Fully specified, ready for an agent without additional human context |
| `ready-for-human` | `ready-for-human` | Requires human implementation |
| `wontfix` | `wontfix` | Will not be actioned |

Use these exact strings when applying triage labels.

Preserve the existing labels and their descriptions. Architecture-handoff child issues continue to use `implement-architecture-slice` as specified by their issue contracts; a readiness label does not replace those contracts.
