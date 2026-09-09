
C1: starting the case predeclared in PLAN.md; `^TestHandshakeRecoversFromCorruption$/^initial$/^corrupt$`. Receipt: `raw/C1-command.json`.

C1: exit 0; `raw/C1.log` and `raw/C1.jsonl`.

C2: starting the case predeclared in PLAN.md; `^TestHandshakeRecoversFromCorruption$/^initial$/^control$`. Receipt: `raw/C2-command.json`.

C2: exit 0; `raw/C2.log` and `raw/C2.jsonl`.

C3: starting the case predeclared in PLAN.md; `^TestHandshakeRecoversFromCorruption$/^handshake$/^corrupt$`. Receipt: `raw/C3-command.json`.

C3: exit 0; `raw/C3.log` and `raw/C3.jsonl`.

C4: starting the case predeclared in PLAN.md; `^TestHandshakeRecoversFromCorruption$/^handshake$/^control$`. Receipt: `raw/C4-command.json`.

C4: exit 0; `raw/C4.log` and `raw/C4.jsonl`.

C5: starting the case predeclared in PLAN.md; `^TestHandshakeCorruption$/^handshake$/^deadline$`. Receipt: `raw/C5-command.json`.

C5: exit 0; `raw/C5.log` and `raw/C5.jsonl`.

V1–V5: starting the five predeclared permanent-case validations without capture, with race detection; one process. Cumulative focused executions after completion: 12, an extension of four beyond the original eight.

V1–V5: exit 0. See `raw/final-race.log`.
