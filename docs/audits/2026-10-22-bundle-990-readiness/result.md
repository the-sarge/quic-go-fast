# #990 Linux readiness result

Candidate and blocking Reno reference: `8095c72244755f0c1e24f4158253bf09cfbe6e17`.
Historical descriptive Reno reference: `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`.

BBRv3 remains experimental, opt-in and unqualified; Reno remains default. This result grants no advancement. The fixture has no batched-send callback, so neither #858’s batch cap nor gluing is measured. macOS and Windows are not qualified.

The independently built candidate/reference Reno plain and diagnostic binaries are identical. Their preservation ratios do not establish preservation against old frozen Reno; that comparison remains descriptive.

The first table uses the intended bundle Reno denominator. Complete cells use medians of five paired ratios. Incomplete cells show the available clean complete blocks and remain evidence gaps; raised values remain raised despite A/A overlap or follow-up outcomes. A/A and old-Reno rows are descriptive.

| Path/workload | Arm/role (blocks) | Metric | Median (min–max) | Crossing blocks | Outcome | A/A min–max |
| --- | --- | --- | --- | --- | --- | --- |
| loopback/stream | cand-bbrv3 (candidate blocking; 5/5) | goodput | 0.897 (0.892–0.903) | 5 | True | 0.993–1.002 |
| loopback/stream | cand-bbrv3 (candidate blocking; 5/5) | send_cpu | 1.115 (1.112–1.123) | 5 | True | 0.995–1.003 |
| loopback/stream | cand-bbrv3 (candidate blocking; 5/5) | receive_cpu | 1.077 (1.064–1.081) | 0 | False | 0.997–1.006 |
| loopback/stream | cand-bbrv3 (candidate blocking; 5/5) | send_rss | 0.816 (0.801–0.832) | 0 | False | 0.973–1.039 |
| loopback/stream | cand-bbrv3 (candidate blocking; 5/5) | receive_rss | 1.080 (1.052–1.099) | 0 | False | 0.964–1.138 |
| loopback/stream | cand-bbrv3 (candidate blocking; 5/5) | control_p95 | 0.464 (0.304–0.629) | 0 | False | 1.156–2.777 |
| loopback/stream | cand-reno (preservation blocking; 5/5) | goodput | 1.000 (0.992–1.001) | 0 | False | 0.993–1.002 |
| loopback/stream | cand-reno (preservation blocking; 5/5) | send_cpu | 1.001 (0.995–1.004) | 0 | False | 0.995–1.003 |
| loopback/stream | cand-reno (preservation blocking; 5/5) | receive_cpu | 1.006 (0.986–1.010) | 0 | False | 0.997–1.006 |
| loopback/stream | cand-reno (preservation blocking; 5/5) | send_rss | 1.024 (0.959–1.047) | 0 | False | 0.973–1.039 |
| loopback/stream | cand-reno (preservation blocking; 5/5) | receive_rss | 1.007 (0.973–1.053) | 0 | False | 0.964–1.138 |
| loopback/stream | cand-reno (preservation blocking; 5/5) | control_p95 | 1.193 (0.253–1.809) | 2 | False | 1.156–2.777 |
| loopback/stream | old-reno (historical descriptive; 5/5) | goodput | 0.775 (0.773–0.777) | 5 | True | 0.993–1.002 |
| loopback/stream | old-reno (historical descriptive; 5/5) | send_cpu | 1.096 (1.093–1.102) | 1 | False | 0.995–1.003 |
| loopback/stream | old-reno (historical descriptive; 5/5) | receive_cpu | 1.157 (1.147–1.163) | 5 | True | 0.997–1.006 |
| loopback/stream | old-reno (historical descriptive; 5/5) | send_rss | 0.846 (0.794–0.879) | 0 | False | 0.973–1.039 |
| loopback/stream | old-reno (historical descriptive; 5/5) | receive_rss | 1.000 (0.962–1.007) | 0 | False | 0.964–1.138 |
| loopback/stream | old-reno (historical descriptive; 5/5) | control_p95 | 0.227 (0.203–0.718) | 0 | False | 1.156–2.777 |
| loopback/stream | reno-reno-aa (A/A descriptive; 5/5) | goodput | 0.999 (0.993–1.002) | 0 | False | 0.993–1.002 |
| loopback/stream | reno-reno-aa (A/A descriptive; 5/5) | send_cpu | 1.000 (0.995–1.003) | 0 | False | 0.995–1.003 |
| loopback/stream | reno-reno-aa (A/A descriptive; 5/5) | receive_cpu | 1.004 (0.997–1.006) | 0 | False | 0.997–1.006 |
| loopback/stream | reno-reno-aa (A/A descriptive; 5/5) | send_rss | 1.016 (0.973–1.039) | 0 | False | 0.973–1.039 |
| loopback/stream | reno-reno-aa (A/A descriptive; 5/5) | receive_rss | 1.005 (0.964–1.138) | 1 | False | 0.964–1.138 |
| loopback/stream | reno-reno-aa (A/A descriptive; 5/5) | control_p95 | 1.998 (1.156–2.777) | 4 | True | 1.156–2.777 |
| loopback/datagram | cand-bbrv3 (candidate blocking; 5/5) | goodput | 0.897 (0.892–0.902) | 5 | True | 0.993–1.009 |
| loopback/datagram | cand-bbrv3 (candidate blocking; 5/5) | send_cpu | 1.090 (1.081–1.096) | 0 | False | 0.991–1.009 |
| loopback/datagram | cand-bbrv3 (candidate blocking; 5/5) | receive_cpu | 1.019 (0.999–1.041) | 0 | False | 0.976–1.045 |
| loopback/datagram | cand-bbrv3 (candidate blocking; 5/5) | send_rss | 0.886 (0.823–0.952) | 0 | False | 0.900–1.066 |
| loopback/datagram | cand-bbrv3 (candidate blocking; 5/5) | receive_rss | 1.074 (1.061–1.197) | 1 | False | 0.978–1.033 |
| loopback/datagram | cand-bbrv3 (candidate blocking; 5/5) | control_p95 | 0.860 (0.170–1.400) | 1 | False | 0.623–4.000 |
| loopback/datagram | cand-reno (preservation blocking; 5/5) | goodput | 0.996 (0.991–1.006) | 0 | False | 0.993–1.009 |
| loopback/datagram | cand-reno (preservation blocking; 5/5) | send_cpu | 1.008 (0.994–1.011) | 0 | False | 0.991–1.009 |
| loopback/datagram | cand-reno (preservation blocking; 5/5) | receive_cpu | 1.007 (0.976–1.045) | 0 | False | 0.976–1.045 |
| loopback/datagram | cand-reno (preservation blocking; 5/5) | send_rss | 0.997 (0.894–1.102) | 1 | False | 0.900–1.066 |
| loopback/datagram | cand-reno (preservation blocking; 5/5) | receive_rss | 0.970 (0.910–1.091) | 0 | False | 0.978–1.033 |
| loopback/datagram | cand-reno (preservation blocking; 5/5) | control_p95 | 1.275 (0.252–2.705) | 3 | True | 0.623–4.000 |
| loopback/datagram | old-reno (historical descriptive; 5/5) | goodput | 0.893 (0.890–0.900) | 5 | True | 0.993–1.009 |
| loopback/datagram | old-reno (historical descriptive; 5/5) | send_cpu | 1.061 (1.050–1.062) | 0 | False | 0.991–1.009 |
| loopback/datagram | old-reno (historical descriptive; 5/5) | receive_cpu | 1.048 (1.040–1.091) | 0 | False | 0.976–1.045 |
| loopback/datagram | old-reno (historical descriptive; 5/5) | send_rss | 0.955 (0.892–0.977) | 0 | False | 0.900–1.066 |
| loopback/datagram | old-reno (historical descriptive; 5/5) | receive_rss | 1.071 (0.996–1.148) | 1 | False | 0.978–1.033 |
| loopback/datagram | old-reno (historical descriptive; 5/5) | control_p95 | 0.851 (0.555–4.667) | 1 | False | 0.623–4.000 |
| loopback/datagram | reno-reno-aa (A/A descriptive; 5/5) | goodput | 0.994 (0.993–1.009) | 0 | False | 0.993–1.009 |
| loopback/datagram | reno-reno-aa (A/A descriptive; 5/5) | send_cpu | 1.007 (0.991–1.009) | 0 | False | 0.991–1.009 |
| loopback/datagram | reno-reno-aa (A/A descriptive; 5/5) | receive_cpu | 1.024 (0.976–1.045) | 0 | False | 0.976–1.045 |
| loopback/datagram | reno-reno-aa (A/A descriptive; 5/5) | send_rss | 0.967 (0.900–1.066) | 0 | False | 0.900–1.066 |
| loopback/datagram | reno-reno-aa (A/A descriptive; 5/5) | receive_rss | 1.002 (0.978–1.033) | 0 | False | 0.978–1.033 |
| loopback/datagram | reno-reno-aa (A/A descriptive; 5/5) | control_p95 | 0.941 (0.623–4.000) | 1 | False | 0.623–4.000 |
| S5/stream | cand-bbrv3 (candidate blocking; 5/5) | goodput | 0.977 (0.971–0.977) | 0 | False | 1.000–1.000 |
| S5/stream | cand-bbrv3 (candidate blocking; 5/5) | send_cpu | 0.963 (0.950–0.976) | 0 | False | 0.991–1.004 |
| S5/stream | cand-bbrv3 (candidate blocking; 5/5) | receive_cpu | 1.045 (1.022–1.060) | 0 | False | 0.990–1.013 |
| S5/stream | cand-bbrv3 (candidate blocking; 5/5) | send_rss | 1.193 (1.157–1.223) | 5 | True | 0.957–1.028 |
| S5/stream | cand-bbrv3 (candidate blocking; 5/5) | receive_rss | 1.248 (1.099–1.439) | 4 | True | 0.983–1.015 |
| S5/stream | cand-bbrv3 (candidate blocking; 5/5) | control_p95 | 0.881 (0.784–1.017) | 0 | False | 0.966–0.992 |
| S5/stream | cand-reno (preservation blocking; 5/5) | goodput | 1.000 (1.000–1.000) | 0 | False | 1.000–1.000 |
| S5/stream | cand-reno (preservation blocking; 5/5) | send_cpu | 1.005 (0.992–1.010) | 0 | False | 0.991–1.004 |
| S5/stream | cand-reno (preservation blocking; 5/5) | receive_cpu | 0.997 (0.985–1.011) | 0 | False | 0.990–1.013 |
| S5/stream | cand-reno (preservation blocking; 5/5) | send_rss | 1.000 (0.949–1.017) | 0 | False | 0.957–1.028 |
| S5/stream | cand-reno (preservation blocking; 5/5) | receive_rss | 0.985 (0.970–1.027) | 0 | False | 0.983–1.015 |
| S5/stream | cand-reno (preservation blocking; 5/5) | control_p95 | 0.985 (0.967–0.989) | 0 | False | 0.966–0.992 |
| S5/stream | old-reno (historical descriptive; 5/5) | goodput | 1.000 (1.000–1.000) | 0 | False | 1.000–1.000 |
| S5/stream | old-reno (historical descriptive; 5/5) | send_cpu | 1.044 (1.036–1.053) | 0 | False | 0.991–1.004 |
| S5/stream | old-reno (historical descriptive; 5/5) | receive_cpu | 0.997 (0.974–1.015) | 0 | False | 0.990–1.013 |
| S5/stream | old-reno (historical descriptive; 5/5) | send_rss | 0.976 (0.945–1.015) | 0 | False | 0.957–1.028 |
| S5/stream | old-reno (historical descriptive; 5/5) | receive_rss | 0.946 (0.943–1.020) | 0 | False | 0.983–1.015 |
| S5/stream | old-reno (historical descriptive; 5/5) | control_p95 | 0.997 (0.970–1.032) | 0 | False | 0.966–0.992 |
| S5/stream | reno-reno-aa (A/A descriptive; 5/5) | goodput | 1.000 (1.000–1.000) | 0 | False | 1.000–1.000 |
| S5/stream | reno-reno-aa (A/A descriptive; 5/5) | send_cpu | 0.998 (0.991–1.004) | 0 | False | 0.991–1.004 |
| S5/stream | reno-reno-aa (A/A descriptive; 5/5) | receive_cpu | 0.995 (0.990–1.013) | 0 | False | 0.990–1.013 |
| S5/stream | reno-reno-aa (A/A descriptive; 5/5) | send_rss | 1.003 (0.957–1.028) | 0 | False | 0.957–1.028 |
| S5/stream | reno-reno-aa (A/A descriptive; 5/5) | receive_rss | 1.000 (0.983–1.015) | 0 | False | 0.983–1.015 |
| S5/stream | reno-reno-aa (A/A descriptive; 5/5) | control_p95 | 0.990 (0.966–0.992) | 0 | False | 0.966–0.992 |
| S5/datagram | cand-bbrv3 (candidate blocking; 5/5) | goodput | 0.971 (0.967–0.977) | 0 | False | 1.000–1.000 |
| S5/datagram | cand-bbrv3 (candidate blocking; 5/5) | send_cpu | 1.000 (0.987–1.004) | 0 | False | 0.989–1.012 |
| S5/datagram | cand-bbrv3 (candidate blocking; 5/5) | receive_cpu | 1.006 (1.002–1.010) | 0 | False | 0.990–1.006 |
| S5/datagram | cand-bbrv3 (candidate blocking; 5/5) | send_rss | 1.149 (1.109–1.168) | 5 | True | 0.959–1.014 |
| S5/datagram | cand-bbrv3 (candidate blocking; 5/5) | receive_rss | 1.065 (1.044–1.078) | 0 | False | 0.984–1.016 |
| S5/datagram | cand-bbrv3 (candidate blocking; 5/5) | control_p95 | 1.008 (0.899–1.011) | 0 | False | 0.988–1.025 |
| S5/datagram | cand-reno (preservation blocking; 5/5) | goodput | 1.000 (1.000–1.000) | 0 | False | 1.000–1.000 |
| S5/datagram | cand-reno (preservation blocking; 5/5) | send_cpu | 0.997 (0.977–1.005) | 0 | False | 0.989–1.012 |
| S5/datagram | cand-reno (preservation blocking; 5/5) | receive_cpu | 1.002 (0.997–1.007) | 0 | False | 0.990–1.006 |
| S5/datagram | cand-reno (preservation blocking; 5/5) | send_rss | 1.011 (0.986–1.032) | 0 | False | 0.959–1.014 |
| S5/datagram | cand-reno (preservation blocking; 5/5) | receive_rss | 0.993 (0.983–1.004) | 0 | False | 0.984–1.016 |
| S5/datagram | cand-reno (preservation blocking; 5/5) | control_p95 | 1.010 (0.977–1.021) | 0 | False | 0.988–1.025 |
| S5/datagram | old-reno (historical descriptive; 5/5) | goodput | 1.000 (1.000–1.000) | 0 | False | 1.000–1.000 |
| S5/datagram | old-reno (historical descriptive; 5/5) | send_cpu | 1.034 (1.019–1.055) | 0 | False | 0.989–1.012 |
| S5/datagram | old-reno (historical descriptive; 5/5) | receive_cpu | 0.997 (0.989–1.005) | 0 | False | 0.990–1.006 |
| S5/datagram | old-reno (historical descriptive; 5/5) | send_rss | 1.001 (0.962–1.175) | 2 | False | 0.959–1.014 |
| S5/datagram | old-reno (historical descriptive; 5/5) | receive_rss | 1.000 (0.986–1.009) | 0 | False | 0.984–1.016 |
| S5/datagram | old-reno (historical descriptive; 5/5) | control_p95 | 1.013 (1.002–1.038) | 0 | False | 0.988–1.025 |
| S5/datagram | reno-reno-aa (A/A descriptive; 5/5) | goodput | 1.000 (1.000–1.000) | 0 | False | 1.000–1.000 |
| S5/datagram | reno-reno-aa (A/A descriptive; 5/5) | send_cpu | 1.004 (0.989–1.012) | 0 | False | 0.989–1.012 |
| S5/datagram | reno-reno-aa (A/A descriptive; 5/5) | receive_cpu | 0.999 (0.990–1.006) | 0 | False | 0.990–1.006 |
| S5/datagram | reno-reno-aa (A/A descriptive; 5/5) | send_rss | 1.000 (0.959–1.014) | 0 | False | 0.959–1.014 |
| S5/datagram | reno-reno-aa (A/A descriptive; 5/5) | receive_rss | 0.997 (0.984–1.016) | 0 | False | 0.984–1.016 |
| S5/datagram | reno-reno-aa (A/A descriptive; 5/5) | control_p95 | 1.009 (0.988–1.025) | 0 | False | 0.988–1.025 |
| S6/stream | cand-bbrv3 (candidate blocking; 5/5) | goodput | 14.061 (12.153–15.622) | — | S6 benefit met (5/5) | 0.982–0.998 |
| S6/stream | cand-bbrv3 (candidate blocking; 5/5) | send_cpu | 0.086 (0.082–0.103) | 0 | False | 0.949–1.010 |
| S6/stream | cand-bbrv3 (candidate blocking; 5/5) | receive_cpu | 0.072 (0.069–0.088) | 0 | False | 0.953–1.003 |
| S6/stream | cand-bbrv3 (candidate blocking; 5/5) | send_rss | 1.697 (1.335–1.716) | 5 | True | 0.970–1.042 |
| S6/stream | cand-bbrv3 (candidate blocking; 5/5) | receive_rss | 1.450 (1.389–1.526) | 5 | True | 0.947–1.017 |
| S6/stream | cand-bbrv3 (candidate blocking; 5/5) | control_p95 | 1.394 (1.316–1.588) | 5 | True | 0.979–1.037 |
| S6/stream | old-reno (historical descriptive; 5/5) | goodput | 0.990 (0.984–1.000) | — | S6 benefit descriptive (0/5) | — |
| S6/stream | old-reno (historical descriptive; 5/5) | send_cpu | 0.989 (0.973–1.000) | 0 | False | 0.949–1.010 |
| S6/stream | old-reno (historical descriptive; 5/5) | receive_cpu | 0.981 (0.966–0.988) | 0 | False | 0.953–1.003 |
| S6/stream | old-reno (historical descriptive; 5/5) | send_rss | 0.985 (0.975–1.021) | 0 | False | 0.970–1.042 |
| S6/stream | old-reno (historical descriptive; 5/5) | receive_rss | 0.971 (0.970–1.031) | 0 | False | 0.947–1.017 |
| S6/stream | old-reno (historical descriptive; 5/5) | control_p95 | 0.973 (0.930–1.114) | 0 | False | 0.979–1.037 |
| S6/stream | reno-reno-aa (A/A descriptive; 5/5) | goodput | 0.991 (0.982–0.998) | — | S6 benefit descriptive (0/5) | — |
| S6/stream | reno-reno-aa (A/A descriptive; 5/5) | send_cpu | 0.980 (0.949–1.010) | 0 | False | 0.949–1.010 |
| S6/stream | reno-reno-aa (A/A descriptive; 5/5) | receive_cpu | 0.971 (0.953–1.003) | 0 | False | 0.953–1.003 |
| S6/stream | reno-reno-aa (A/A descriptive; 5/5) | send_rss | 1.039 (0.970–1.042) | 0 | False | 0.970–1.042 |
| S6/stream | reno-reno-aa (A/A descriptive; 5/5) | receive_rss | 1.004 (0.947–1.017) | 0 | False | 0.947–1.017 |
| S6/stream | reno-reno-aa (A/A descriptive; 5/5) | control_p95 | 0.989 (0.979–1.037) | 0 | False | 0.979–1.037 |
| S6/datagram | cand-bbrv3 (candidate blocking; 5/5) | goodput | 14.268 (11.784–15.582) | — | S6 benefit met (5/5) | 0.964–1.009 |
| S6/datagram | cand-bbrv3 (candidate blocking; 5/5) | send_cpu | 0.096 (0.087–0.107) | 0 | False | 0.988–1.091 |
| S6/datagram | cand-bbrv3 (candidate blocking; 5/5) | receive_cpu | 0.082 (0.074–0.095) | 0 | False | 0.992–1.098 |
| S6/datagram | cand-bbrv3 (candidate blocking; 5/5) | send_rss | 1.509 (1.382–1.547) | 5 | True | 0.987–1.051 |
| S6/datagram | cand-bbrv3 (candidate blocking; 5/5) | receive_rss | 1.095 (1.074–1.104) | 2 | False | 0.962–1.039 |
| S6/datagram | cand-bbrv3 (candidate blocking; 5/5) | control_p95 | 1.336 (1.319–1.371) | 5 | True | 0.944–1.059 |
| S6/datagram | old-reno (historical descriptive; 5/5) | goodput | 1.007 (0.970–1.027) | — | S6 benefit descriptive (3/5) | — |
| S6/datagram | old-reno (historical descriptive; 5/5) | send_cpu | 0.993 (0.976–1.076) | 0 | False | 0.988–1.091 |
| S6/datagram | old-reno (historical descriptive; 5/5) | receive_cpu | 1.006 (0.987–1.065) | 0 | False | 0.992–1.098 |
| S6/datagram | old-reno (historical descriptive; 5/5) | send_rss | 0.988 (0.969–1.017) | 0 | False | 0.987–1.051 |
| S6/datagram | old-reno (historical descriptive; 5/5) | receive_rss | 0.970 (0.953–1.020) | 0 | False | 0.962–1.039 |
| S6/datagram | old-reno (historical descriptive; 5/5) | control_p95 | 0.971 (0.945–1.036) | 0 | False | 0.944–1.059 |
| S6/datagram | reno-reno-aa (A/A descriptive; 5/5) | goodput | 0.989 (0.964–1.009) | — | S6 benefit descriptive (2/5) | — |
| S6/datagram | reno-reno-aa (A/A descriptive; 5/5) | send_cpu | 1.031 (0.988–1.091) | 0 | False | 0.988–1.091 |
| S6/datagram | reno-reno-aa (A/A descriptive; 5/5) | receive_cpu | 1.030 (0.992–1.098) | 0 | False | 0.992–1.098 |
| S6/datagram | reno-reno-aa (A/A descriptive; 5/5) | send_rss | 1.013 (0.987–1.051) | 0 | False | 0.987–1.051 |
| S6/datagram | reno-reno-aa (A/A descriptive; 5/5) | receive_rss | 0.994 (0.962–1.039) | 0 | False | 0.962–1.039 |
| S6/datagram | reno-reno-aa (A/A descriptive; 5/5) | control_p95 | 0.995 (0.944–1.059) | 0 | False | 0.944–1.059 |

Blocking outcomes (including unmet S6 benefit and evidence gaps):

```json
[
  {
    "arm": "cand-bbrv3",
    "criterion": "goodput",
    "median": 0.8968789844584582,
    "outcome": "raised",
    "path": "loopback",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "send_cpu",
    "median": 1.1145589221213064,
    "outcome": "raised",
    "path": "loopback",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "goodput",
    "median": 0.8970198974290448,
    "outcome": "raised",
    "path": "loopback",
    "workload": "datagram"
  },
  {
    "arm": "cand-reno",
    "criterion": "control_p95",
    "median": 1.2748054111190597,
    "outcome": "raised",
    "path": "loopback",
    "workload": "datagram"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "send_rss",
    "median": 1.1926467426359924,
    "outcome": "raised",
    "path": "S5",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "receive_rss",
    "median": 1.2475540386803186,
    "outcome": "raised",
    "path": "S5",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "send_rss",
    "median": 1.1485739553393766,
    "outcome": "raised",
    "path": "S5",
    "workload": "datagram"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "send_rss",
    "median": 1.6967039612942243,
    "outcome": "raised",
    "path": "S6",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "receive_rss",
    "median": 1.4504630928992421,
    "outcome": "raised",
    "path": "S6",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "control_p95",
    "median": 1.393584616426547,
    "outcome": "raised",
    "path": "S6",
    "workload": "stream"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "send_rss",
    "median": 1.5091428571428571,
    "outcome": "raised",
    "path": "S6",
    "workload": "datagram"
  },
  {
    "arm": "cand-bbrv3",
    "criterion": "control_p95",
    "median": 1.3361818246057375,
    "outcome": "raised",
    "path": "S6",
    "workload": "datagram"
  }
]
```

## Coverage and retained gaps

Only complete, usable, clean blocks enter selected statistics. Missing, fatal and unresolved attempts remain below and in the raw archive. An incomplete row is not a pass.

| Stage/workload | Clean blocks / expected | Status |
| --- | --- | --- |
| loopback/datagram | 5/5 | complete observations |
| loopback/stream | 5/5 | complete observations |
| preslat-datagram/datagram | 6/6 | complete observations |
| s5/datagram | 5/5 | complete observations |
| s5/stream | 5/5 | complete observations |
| s5timeline/datagram | 4/4 | complete observations |
| s5timeline/stream | 4/4 | complete observations |
| s6/datagram | 5/5 | complete observations |
| s6/stream | 5/5 | complete observations |

Retained gaps:

```json
[]
```

Conditional selection and omissions: pairs in `not_run_presrss` and workloads in `not_run_preslat` are **not run (selection limit), evidence gap**. Their raised preservation values remain blocking.

```json
{
  "not_run_preslat": [],
  "not_run_presrss": [],
  "readiness_sha256": "ede13a51918aa762f8ba114112acc9acf3651b21d1d41fd2cdbe92e317808197",
  "selected": [
    "preslat-datagram"
  ]
}
```


Original-only block inventory (known contaminated complete blocks remain descriptive; invalid or partial originals remain gaps):

```json
[
  {
    "block": 1,
    "directories": [
      "observations/main-loopback-stream-p1-cand-bbrv3",
      "observations/main-loopback-stream-p1-cand-reno",
      "observations/main-loopback-stream-p1-old-reno",
      "observations/main-loopback-stream-p1-reno-reno",
      "observations/main-loopback-stream-p1-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 2,
    "directories": [
      "observations/main-loopback-stream-p2-cand-bbrv3",
      "observations/main-loopback-stream-p2-cand-reno",
      "observations/main-loopback-stream-p2-old-reno",
      "observations/main-loopback-stream-p2-reno-reno",
      "observations/main-loopback-stream-p2-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 3,
    "directories": [
      "observations/main-loopback-stream-p3-cand-bbrv3",
      "observations/main-loopback-stream-p3-cand-reno",
      "observations/main-loopback-stream-p3-old-reno",
      "observations/main-loopback-stream-p3-reno-reno",
      "observations/main-loopback-stream-p3-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 4,
    "directories": [
      "observations/main-loopback-stream-p4-cand-bbrv3",
      "observations/main-loopback-stream-p4-cand-reno",
      "observations/main-loopback-stream-p4-old-reno",
      "observations/main-loopback-stream-p4-reno-reno",
      "observations/main-loopback-stream-p4-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 5,
    "directories": [
      "observations/main-loopback-stream-p5-cand-bbrv3",
      "observations/main-loopback-stream-p5-cand-reno",
      "observations/main-loopback-stream-p5-old-reno",
      "observations/main-loopback-stream-p5-reno-reno",
      "observations/main-loopback-stream-p5-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 1,
    "directories": [
      "observations/main-loopback-datagram-p1-cand-bbrv3",
      "observations/main-loopback-datagram-p1-cand-reno",
      "observations/main-loopback-datagram-p1-old-reno",
      "observations/main-loopback-datagram-p1-reno-reno",
      "observations/main-loopback-datagram-p1-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 2,
    "directories": [
      "observations/main-loopback-datagram-p2-cand-bbrv3",
      "observations/main-loopback-datagram-p2-cand-reno",
      "observations/main-loopback-datagram-p2-old-reno",
      "observations/main-loopback-datagram-p2-reno-reno",
      "observations/main-loopback-datagram-p2-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 3,
    "directories": [
      "observations/main-loopback-datagram-p3-cand-bbrv3",
      "observations/main-loopback-datagram-p3-cand-reno",
      "observations/main-loopback-datagram-p3-old-reno",
      "observations/main-loopback-datagram-p3-reno-reno",
      "observations/main-loopback-datagram-p3-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 4,
    "directories": [
      "observations/main-loopback-datagram-p4-cand-bbrv3",
      "observations/main-loopback-datagram-p4-cand-reno",
      "observations/main-loopback-datagram-p4-old-reno",
      "observations/main-loopback-datagram-p4-reno-reno",
      "observations/main-loopback-datagram-p4-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 5,
    "directories": [
      "observations/main-loopback-datagram-p5-cand-bbrv3",
      "observations/main-loopback-datagram-p5-cand-reno",
      "observations/main-loopback-datagram-p5-old-reno",
      "observations/main-loopback-datagram-p5-reno-reno",
      "observations/main-loopback-datagram-p5-reno-reno-aa"
    ],
    "stage": "loopback",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 1,
    "directories": [
      "observations/main-S5-stream-p1-cand-bbrv3",
      "observations/main-S5-stream-p1-cand-reno",
      "observations/main-S5-stream-p1-old-reno",
      "observations/main-S5-stream-p1-reno-reno",
      "observations/main-S5-stream-p1-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 2,
    "directories": [
      "observations/main-S5-stream-p2-cand-bbrv3",
      "observations/main-S5-stream-p2-cand-reno",
      "observations/main-S5-stream-p2-old-reno",
      "observations/main-S5-stream-p2-reno-reno",
      "observations/main-S5-stream-p2-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 3,
    "directories": [
      "observations/main-S5-stream-p3-cand-bbrv3",
      "observations/main-S5-stream-p3-cand-reno",
      "observations/main-S5-stream-p3-old-reno",
      "observations/main-S5-stream-p3-reno-reno",
      "observations/main-S5-stream-p3-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 4,
    "directories": [
      "observations/main-S5-stream-p4-cand-bbrv3",
      "observations/main-S5-stream-p4-cand-reno",
      "observations/main-S5-stream-p4-old-reno",
      "observations/main-S5-stream-p4-reno-reno",
      "observations/main-S5-stream-p4-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 5,
    "directories": [
      "observations/main-S5-stream-p5-cand-bbrv3",
      "observations/main-S5-stream-p5-cand-reno",
      "observations/main-S5-stream-p5-old-reno",
      "observations/main-S5-stream-p5-reno-reno",
      "observations/main-S5-stream-p5-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 1,
    "directories": [
      "observations/main-S5-datagram-p1-cand-bbrv3",
      "observations/main-S5-datagram-p1-cand-reno",
      "observations/main-S5-datagram-p1-old-reno",
      "observations/main-S5-datagram-p1-reno-reno",
      "observations/main-S5-datagram-p1-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 2,
    "directories": [
      "observations/main-S5-datagram-p2-cand-bbrv3",
      "observations/main-S5-datagram-p2-cand-reno",
      "observations/main-S5-datagram-p2-old-reno",
      "observations/main-S5-datagram-p2-reno-reno",
      "observations/main-S5-datagram-p2-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 3,
    "directories": [
      "observations/main-S5-datagram-p3-cand-bbrv3",
      "observations/main-S5-datagram-p3-cand-reno",
      "observations/main-S5-datagram-p3-old-reno",
      "observations/main-S5-datagram-p3-reno-reno",
      "observations/main-S5-datagram-p3-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 4,
    "directories": [
      "observations/main-S5-datagram-p4-cand-bbrv3",
      "observations/main-S5-datagram-p4-cand-reno",
      "observations/main-S5-datagram-p4-old-reno",
      "observations/main-S5-datagram-p4-reno-reno",
      "observations/main-S5-datagram-p4-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 5,
    "directories": [
      "observations/main-S5-datagram-p5-cand-bbrv3",
      "observations/main-S5-datagram-p5-cand-reno",
      "observations/main-S5-datagram-p5-old-reno",
      "observations/main-S5-datagram-p5-reno-reno",
      "observations/main-S5-datagram-p5-reno-reno-aa"
    ],
    "stage": "s5",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 1,
    "directories": [
      "observations/main-S6-stream-p1-cand-bbrv3",
      "observations/main-S6-stream-p1-old-reno",
      "observations/main-S6-stream-p1-reno-reno",
      "observations/main-S6-stream-p1-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 2,
    "directories": [
      "observations/main-S6-stream-p2-cand-bbrv3",
      "observations/main-S6-stream-p2-old-reno",
      "observations/main-S6-stream-p2-reno-reno",
      "observations/main-S6-stream-p2-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 3,
    "directories": [
      "observations/main-S6-stream-p3-cand-bbrv3",
      "observations/main-S6-stream-p3-old-reno",
      "observations/main-S6-stream-p3-reno-reno",
      "observations/main-S6-stream-p3-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 4,
    "directories": [
      "observations/main-S6-stream-p4-cand-bbrv3",
      "observations/main-S6-stream-p4-old-reno",
      "observations/main-S6-stream-p4-reno-reno",
      "observations/main-S6-stream-p4-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 5,
    "directories": [
      "observations/main-S6-stream-p5-cand-bbrv3",
      "observations/main-S6-stream-p5-old-reno",
      "observations/main-S6-stream-p5-reno-reno",
      "observations/main-S6-stream-p5-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "stream"
  },
  {
    "block": 1,
    "directories": [
      "observations/main-S6-datagram-p1-cand-bbrv3",
      "observations/main-S6-datagram-p1-old-reno",
      "observations/main-S6-datagram-p1-reno-reno",
      "observations/main-S6-datagram-p1-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 2,
    "directories": [
      "observations/main-S6-datagram-p2-cand-bbrv3",
      "observations/main-S6-datagram-p2-old-reno",
      "observations/main-S6-datagram-p2-reno-reno",
      "observations/main-S6-datagram-p2-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 3,
    "directories": [
      "observations/main-S6-datagram-p3-cand-bbrv3",
      "observations/main-S6-datagram-p3-old-reno",
      "observations/main-S6-datagram-p3-reno-reno",
      "observations/main-S6-datagram-p3-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 4,
    "directories": [
      "observations/main-S6-datagram-p4-cand-bbrv3",
      "observations/main-S6-datagram-p4-old-reno",
      "observations/main-S6-datagram-p4-reno-reno",
      "observations/main-S6-datagram-p4-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "datagram"
  },
  {
    "block": 5,
    "directories": [
      "observations/main-S6-datagram-p5-cand-bbrv3",
      "observations/main-S6-datagram-p5-old-reno",
      "observations/main-S6-datagram-p5-reno-reno",
      "observations/main-S6-datagram-p5-reno-reno-aa"
    ],
    "stage": "s6",
    "status": "clean",
    "workload": "datagram"
  }
]
```

## Absolute medians

| Path/workload | Arm | Goodput Mbit/s | CPU s/GiB send/receive | RSS MiB send/receive | Control p95/p50 ms |
| --- | --- | --- | --- | --- | --- |
| loopback/stream | cand-bbrv3 | 5452.829 | 4.019/2.543 | 15.516/15.828 | 0.264/0.132 |
| loopback/stream | cand-reno | 6075.494 | 3.610/2.378 | 19.125/14.762 | 0.546/0.131 |
| loopback/stream | old-reno | 4715.726 | 3.950/2.738 | 15.977/14.523 | 0.191/0.066 |
| loopback/stream | reno-reno | 6079.946 | 3.606/2.361 | 18.895/14.656 | 0.570/0.138 |
| loopback/stream | reno-reno-aa | 6076.515 | 3.608/2.371 | 19.203/14.730 | 0.974/0.149 |
| loopback/datagram | cand-bbrv3 | 4667.171 | 4.654/2.908 | 16.676/20.293 | 0.302/0.172 |
| loopback/datagram | cand-reno | 5193.944 | 4.293/2.893 | 18.461/18.543 | 0.375/0.152 |
| loopback/datagram | old-reno | 4650.620 | 4.520/3.012 | 17.594/20.219 | 0.318/0.089 |
| loopback/datagram | reno-reno | 5212.630 | 4.264/2.854 | 18.508/18.871 | 0.341/0.173 |
| loopback/datagram | reno-reno-aa | 5188.737 | 4.294/2.893 | 18.430/18.703 | 0.342/0.156 |
| S5/stream | cand-bbrv3 | 93.656 | 9.365/8.928 | 21.668/21.418 | 163.131/102.520 |
| S5/stream | cand-reno | 95.886 | 9.802/8.593 | 17.820/16.918 | 181.307/173.990 |
| S5/stream | old-reno | 95.890 | 10.232/8.550 | 17.703/16.191 | 182.847/177.120 |
| S5/stream | reno-reno | 95.890 | 9.757/8.588 | 17.918/16.918 | 184.130/177.794 |
| S5/stream | reno-reno-aa | 95.890 | 9.752/8.585 | 18.117/16.918 | 181.599/175.834 |
| S5/datagram | cand-bbrv3 | 91.704 | 10.950/8.820 | 20.500/15.254 | 177.843/102.171 |
| S5/datagram | cand-reno | 94.412 | 10.951/8.779 | 18.168/14.270 | 178.059/172.301 |
| S5/datagram | old-reno | 94.412 | 11.375/8.757 | 18.191/14.309 | 180.214/173.877 |
| S5/datagram | reno-reno | 94.412 | 10.997/8.766 | 17.918/14.324 | 177.344/170.644 |
| S5/datagram | reno-reno-aa | 94.412 | 11.042/8.782 | 17.672/14.211 | 178.098/172.270 |
| S6/stream | cand-bbrv3 | 87.728 | 9.546/8.863 | 21.918/20.188 | 176.530/101.971 |
| S6/stream | old-reno | 6.115 | 109.956/120.881 | 13.500/13.754 | 124.277/105.178 |
| S6/stream | reno-reno | 6.215 | 111.188/123.066 | 13.418/14.168 | 126.673/104.814 |
| S6/stream | reno-reno-aa | 6.102 | 105.505/119.482 | 13.980/13.977 | 124.494/105.401 |
| S6/datagram | cand-bbrv3 | 85.563 | 10.786/9.250 | 20.832/15.348 | 166.930/102.000 |
| S6/datagram | old-reno | 6.067 | 117.048/115.298 | 13.691/13.625 | 122.869/104.217 |
| S6/datagram | reno-reno | 5.921 | 112.065/113.376 | 13.844/13.957 | 124.296/103.831 |
| S6/datagram | reno-reno-aa | 5.931 | 119.738/117.783 | 14.121/13.918 | 124.363/104.227 |

## Per-block ratios

Every six-value array is goodput, sender CPU, receiver CPU, sender RSS, receiver RSS and control p95, in that order.

| Path/workload/block | Arm | Versus bundle Reno |
| --- | --- | --- |
| loopback/stream/1 | cand-bbrv3 | 0.894, 1.115, 1.077, 0.822, 1.099, 0.557 |
| loopback/stream/1 | cand-reno | 0.992, 0.995, 1.010, 1.047, 1.019, 1.239 |
| loopback/stream/1 | old-reno | 0.773, 1.097, 1.163, 0.846, 1.001, 0.222 |
| loopback/stream/1 | reno-reno-aa | 0.993, 0.995, 1.006, 1.039, 1.001, 1.289 |
| loopback/stream/2 | cand-bbrv3 | 0.892, 1.123, 1.077, 0.801, 1.087, 0.464 |
| loopback/stream/2 | cand-reno | 1.000, 1.004, 1.006, 0.959, 1.053, 0.571 |
| loopback/stream/2 | old-reno | 0.775, 1.096, 1.157, 0.822, 1.000, 0.393 |
| loopback/stream/2 | reno-reno-aa | 0.999, 1.001, 1.004, 0.993, 1.014, 1.998 |
| loopback/stream/3 | cand-bbrv3 | 0.903, 1.112, 1.074, 0.808, 1.052, 0.324 |
| loopback/stream/3 | cand-reno | 0.997, 1.002, 1.010, 0.969, 0.992, 0.253 |
| loopback/stream/3 | old-reno | 0.776, 1.095, 1.161, 0.794, 0.962, 0.227 |
| loopback/stream/3 | reno-reno-aa | 0.999, 1.003, 1.006, 0.973, 0.964, 1.156 |
| loopback/stream/4 | cand-bbrv3 | 0.898, 1.114, 1.064, 0.832, 1.080, 0.304 |
| loopback/stream/4 | cand-reno | 1.000, 1.001, 0.986, 1.024, 1.007, 1.193 |
| loopback/stream/4 | old-reno | 0.774, 1.102, 1.147, 0.856, 1.007, 0.203 |
| loopback/stream/4 | reno-reno-aa | 1.002, 0.999, 0.997, 1.025, 1.005, 2.041 |
| loopback/stream/5 | cand-bbrv3 | 0.897, 1.116, 1.081, 0.816, 1.057, 0.629 |
| loopback/stream/5 | cand-reno | 1.001, 0.998, 1.003, 1.043, 0.973, 1.809 |
| loopback/stream/5 | old-reno | 0.777, 1.093, 1.156, 0.879, 0.974, 0.718 |
| loopback/stream/5 | reno-reno-aa | 1.001, 1.000, 1.003, 1.016, 1.138, 2.777 |
| loopback/datagram/1 | cand-bbrv3 | 0.892, 1.094, 1.016, 0.850, 1.061, 0.886 |
| loopback/datagram/1 | cand-reno | 0.994, 1.008, 1.007, 0.905, 0.970, 0.725 |
| loopback/datagram/1 | old-reno | 0.890, 1.061, 1.048, 0.892, 0.996, 4.667 |
| loopback/datagram/1 | reno-reno-aa | 0.993, 1.006, 0.993, 0.900, 0.978, 1.004 |
| loopback/datagram/2 | cand-bbrv3 | 0.898, 1.090, 1.029, 0.952, 1.065, 0.860 |
| loopback/datagram/2 | cand-reno | 0.991, 1.011, 1.026, 1.102, 0.964, 1.275 |
| loopback/datagram/2 | old-reno | 0.893, 1.061, 1.055, 0.977, 1.071, 1.022 |
| loopback/datagram/2 | reno-reno-aa | 0.994, 1.007, 1.024, 1.066, 1.005, 0.941 |
| loopback/datagram/3 | cand-bbrv3 | 0.897, 1.087, 1.041, 0.823, 1.074, 1.400 |
| loopback/datagram/3 | cand-reno | 0.999, 1.004, 1.045, 0.894, 0.910, 1.491 |
| loopback/datagram/3 | old-reno | 0.891, 1.062, 1.091, 0.897, 1.018, 0.764 |
| loopback/datagram/3 | reno-reno-aa | 0.993, 1.009, 1.045, 0.939, 0.978, 4.000 |
| loopback/datagram/4 | cand-bbrv3 | 0.902, 1.081, 0.999, 0.886, 1.197, 0.815 |
| loopback/datagram/4 | cand-reno | 1.006, 0.994, 0.976, 1.015, 1.091, 2.705 |
| loopback/datagram/4 | old-reno | 0.900, 1.050, 1.040, 0.958, 1.148, 0.851 |
| loopback/datagram/4 | reno-reno-aa | 1.009, 0.991, 0.976, 0.967, 1.033, 0.883 |
| loopback/datagram/5 | cand-bbrv3 | 0.895, 1.096, 1.019, 0.902, 1.076, 0.170 |
| loopback/datagram/5 | cand-reno | 0.996, 1.008, 1.006, 0.997, 1.045, 0.252 |
| loopback/datagram/5 | old-reno | 0.894, 1.057, 1.043, 0.955, 1.087, 0.555 |
| loopback/datagram/5 | reno-reno-aa | 0.997, 1.007, 1.026, 1.019, 1.002, 0.623 |
| S5/stream/1 | cand-bbrv3 | 0.971, 0.970, 1.030, 1.223, 1.439, 0.881 |
| S5/stream/1 | cand-reno | 1.000, 1.008, 0.995, 1.000, 0.985, 0.989 |
| S5/stream/1 | old-reno | 1.000, 1.053, 0.989, 1.015, 1.020, 1.008 |
| S5/stream/1 | reno-reno-aa | 1.000, 1.000, 0.994, 1.000, 1.000, 0.991 |
| S5/stream/2 | cand-bbrv3 | 0.977, 0.950, 1.045, 1.193, 1.099, 0.856 |
| S5/stream/2 | cand-reno | 1.000, 1.000, 1.011, 0.981, 0.983, 0.975 |
| S5/stream/2 | old-reno | 1.000, 1.044, 1.001, 0.976, 0.944, 0.992 |
| S5/stream/2 | reno-reno-aa | 1.000, 0.996, 1.012, 1.003, 0.983, 0.990 |
| S5/stream/3 | cand-bbrv3 | 0.971, 0.976, 1.049, 1.157, 1.148, 0.784 |
| S5/stream/3 | cand-reno | 1.000, 1.010, 0.997, 0.949, 1.027, 0.967 |
| S5/stream/3 | old-reno | 1.000, 1.036, 0.997, 0.945, 0.982, 0.997 |
| S5/stream/3 | reno-reno-aa | 1.000, 0.998, 0.995, 0.957, 1.000, 0.966 |
| S5/stream/4 | cand-bbrv3 | 0.977, 0.963, 1.060, 1.185, 1.248, 1.017 |
| S5/stream/4 | cand-reno | 1.000, 1.005, 1.007, 1.017, 1.000, 0.987 |
| S5/stream/4 | old-reno | 1.000, 1.044, 1.015, 0.974, 0.943, 0.970 |
| S5/stream/4 | reno-reno-aa | 1.000, 1.004, 1.013, 1.028, 0.985, 0.992 |
| S5/stream/5 | cand-bbrv3 | 0.977, 0.952, 1.022, 1.216, 1.417, 0.948 |
| S5/stream/5 | cand-reno | 1.000, 0.992, 0.985, 1.001, 0.970, 0.985 |
| S5/stream/5 | old-reno | 1.000, 1.051, 0.974, 1.001, 0.946, 1.032 |
| S5/stream/5 | reno-reno-aa | 1.000, 0.991, 0.990, 1.026, 1.015, 0.978 |
| S5/datagram/1 | cand-bbrv3 | 0.977, 0.995, 1.005, 1.168, 1.053, 1.011 |
| S5/datagram/1 | cand-reno | 1.000, 0.998, 1.004, 1.014, 0.983, 1.018 |
| S5/datagram/1 | old-reno | 1.000, 1.034, 1.005, 1.001, 0.986, 1.011 |
| S5/datagram/1 | reno-reno-aa | 1.000, 1.009, 1.006, 1.000, 0.997, 1.009 |
| S5/datagram/2 | cand-bbrv3 | 0.977, 1.004, 1.010, 1.164, 1.065, 1.008 |
| S5/datagram/2 | cand-reno | 1.000, 1.005, 1.001, 1.011, 0.996, 1.021 |
| S5/datagram/2 | old-reno | 1.000, 1.055, 1.000, 0.962, 1.000, 1.002 |
| S5/datagram/2 | reno-reno-aa | 1.000, 1.004, 0.995, 1.014, 0.984, 1.002 |
| S5/datagram/3 | cand-bbrv3 | 0.971, 1.004, 1.002, 1.109, 1.071, 1.009 |
| S5/datagram/3 | cand-reno | 1.000, 0.994, 1.002, 0.986, 0.993, 1.010 |
| S5/datagram/3 | old-reno | 1.000, 1.020, 0.996, 0.988, 0.996, 1.031 |
| S5/datagram/3 | reno-reno-aa | 1.000, 0.989, 0.999, 0.959, 0.989, 0.988 |
| S5/datagram/4 | cand-bbrv3 | 0.971, 0.987, 1.010, 1.128, 1.078, 0.997 |
| S5/datagram/4 | cand-reno | 1.000, 0.977, 1.007, 0.986, 1.004, 0.977 |
| S5/datagram/4 | old-reno | 1.000, 1.019, 0.997, 1.115, 1.001, 1.013 |
| S5/datagram/4 | reno-reno-aa | 1.000, 0.993, 1.006, 0.973, 0.998, 1.025 |
| S5/datagram/5 | cand-bbrv3 | 0.967, 1.000, 1.006, 1.149, 1.044, 0.899 |
| S5/datagram/5 | cand-reno | 1.000, 0.997, 0.997, 1.032, 0.984, 1.006 |
| S5/datagram/5 | old-reno | 1.000, 1.046, 0.989, 1.175, 1.009, 1.038 |
| S5/datagram/5 | reno-reno-aa | 1.000, 1.012, 0.990, 1.000, 1.016, 1.017 |
| S6/stream/1 | cand-bbrv3 | 14.765, 0.084, 0.070, 1.335, 1.526, 1.588 |
| S6/stream/1 | old-reno | 0.993, 1.000, 0.969, 0.975, 1.031, 1.086 |
| S6/stream/1 | reno-reno-aa | 0.988, 1.002, 0.967, 1.000, 1.010, 1.037 |
| S6/stream/2 | cand-bbrv3 | 12.153, 0.103, 0.088, 1.708, 1.421, 1.316 |
| S6/stream/2 | old-reno | 0.990, 0.995, 0.988, 1.006, 0.971, 0.971 |
| S6/stream/2 | reno-reno-aa | 0.991, 1.010, 1.003, 1.042, 0.947, 1.009 |
| S6/stream/3 | cand-bbrv3 | 12.211, 0.098, 0.085, 1.486, 1.389, 1.394 |
| S6/stream/3 | old-reno | 0.988, 0.984, 0.981, 0.985, 0.989, 0.930 |
| S6/stream/3 | reno-reno-aa | 0.998, 0.980, 0.953, 0.970, 1.017, 0.979 |
| S6/stream/4 | cand-bbrv3 | 15.622, 0.082, 0.069, 1.716, 1.450, 1.455 |
| S6/stream/4 | old-reno | 1.000, 0.973, 0.966, 1.021, 0.970, 0.973 |
| S6/stream/4 | reno-reno-aa | 0.995, 0.977, 1.003, 1.039, 0.951, 0.983 |
| S6/stream/5 | cand-bbrv3 | 14.061, 0.086, 0.072, 1.697, 1.464, 1.320 |
| S6/stream/5 | old-reno | 0.984, 0.989, 0.982, 0.982, 0.970, 1.114 |
| S6/stream/5 | reno-reno-aa | 0.982, 0.949, 0.971, 1.039, 1.004, 0.989 |
| S6/datagram/1 | cand-bbrv3 | 14.268, 0.096, 0.082, 1.382, 1.078, 1.371 |
| S6/datagram/1 | old-reno | 1.025, 1.044, 1.017, 0.969, 0.953, 1.036 |
| S6/datagram/1 | reno-reno-aa | 1.002, 1.068, 1.039, 0.987, 0.962, 1.012 |
| S6/datagram/2 | cand-bbrv3 | 12.334, 0.107, 0.093, 1.472, 1.074, 1.319 |
| S6/datagram/2 | old-reno | 0.970, 0.989, 0.994, 0.985, 0.953, 0.971 |
| S6/datagram/2 | reno-reno-aa | 0.964, 0.988, 1.006, 1.005, 0.985, 1.059 |
| S6/datagram/3 | cand-bbrv3 | 11.784, 0.107, 0.095, 1.547, 1.104, 1.336 |
| S6/datagram/3 | old-reno | 1.027, 0.976, 0.987, 0.988, 0.970, 0.953 |
| S6/datagram/3 | reno-reno-aa | 0.989, 0.995, 0.992, 1.020, 0.994, 0.995 |
| S6/datagram/4 | cand-bbrv3 | 15.582, 0.087, 0.074, 1.524, 1.101, 1.371 |
| S6/datagram/4 | old-reno | 0.975, 1.076, 1.065, 1.017, 1.020, 1.035 |
| S6/datagram/4 | reno-reno-aa | 0.979, 1.091, 1.098, 1.051, 1.001, 0.944 |
| S6/datagram/5 | cand-bbrv3 | 15.341, 0.087, 0.075, 1.509, 1.095, 1.323 |
| S6/datagram/5 | old-reno | 1.007, 0.993, 1.006, 0.997, 0.987, 0.945 |
| S6/datagram/5 | reno-reno-aa | 1.009, 1.031, 1.030, 1.013, 1.039, 0.969 |

## Historical comparison (descriptive)

Current values below divide by the same-block old frozen Reno. Historical r9, port and #808 values divide by their own old frozen Reno observations, on different days. These are descriptive comparisons, never replacements for the current blocking-reference table. This also reports #738’s carried S5 DATAGRAM sender CPU cost and #734’s CPU-per-useful-GiB proxy for wake cost; wake counts are not measured.

| Path/workload | Arm/metric | Current old-reference ratio | r9 | Port | #808 |
| --- | --- | --- | --- | --- | --- |
| loopback/stream | cand-bbrv3/goodput | 1.156 | 1.148 | 1.160 | 1.160 |
| loopback/stream | cand-bbrv3/send_cpu | 1.016 | 1.007 | 1.014 | 1.015 |
| loopback/stream | cand-bbrv3/receive_cpu | 0.927 | 0.937 | 0.938 | 0.940 |
| loopback/stream | cand-bbrv3/send_rss | 0.972 | 0.954 | 0.974 | 0.968 |
| loopback/stream | cand-bbrv3/receive_rss | 1.088 | 1.120 | 1.095 | 1.094 |
| loopback/stream | cand-bbrv3/control_p95 | 1.430 | 1.485 | 1.656 | 1.981 |
| loopback/stream | cand-reno/goodput | 1.288 | 1.003 | 0.998 | 0.998 |
| loopback/stream | cand-reno/send_cpu | 0.913 | 1.000 | 1.003 | 1.003 |
| loopback/stream | cand-reno/receive_cpu | 0.869 | 1.000 | 1.003 | 1.001 |
| loopback/stream | cand-reno/send_rss | 1.197 | 1.006 | 0.998 | 1.010 |
| loopback/stream | cand-reno/receive_rss | 1.018 | 0.998 | 1.002 | 1.005 |
| loopback/stream | cand-reno/control_p95 | 2.519 | 0.930 | 0.880 | 0.879 |
| loopback/datagram | cand-bbrv3/goodput | 1.002 | 1.001 | 0.995 | 0.996 |
| loopback/datagram | cand-bbrv3/send_cpu | 1.030 | 1.022 | 1.028 | 1.025 |
| loopback/datagram | cand-bbrv3/receive_cpu | 0.969 | 0.964 | 0.968 | 0.967 |
| loopback/datagram | cand-bbrv3/send_rss | 0.944 | 0.956 | 0.949 | 0.961 |
| loopback/datagram | cand-bbrv3/receive_rss | 1.043 | 1.148 | 1.098 | 1.069 |
| loopback/datagram | cand-bbrv3/control_p95 | 0.841 | 0.868 | 0.695 | 0.776 |
| loopback/datagram | cand-reno/goodput | 1.117 | 0.999 | 0.999 | 0.996 |
| loopback/datagram | cand-reno/send_cpu | 0.950 | 1.000 | 1.002 | 1.000 |
| loopback/datagram | cand-reno/receive_cpu | 0.960 | 1.002 | 0.998 | 0.994 |
| loopback/datagram | cand-reno/send_rss | 1.044 | 0.998 | 0.996 | 1.026 |
| loopback/datagram | cand-reno/receive_rss | 0.950 | 1.044 | 0.987 | 0.996 |
| loopback/datagram | cand-reno/control_p95 | 1.247 | 0.744 | 1.504 | 0.720 |
| S5/stream | cand-bbrv3/goodput | 0.977 | 0.967 | 0.972 | 0.971 |
| S5/stream | cand-bbrv3/send_cpu | 0.921 | 0.937 | 0.927 | 0.929 |
| S5/stream | cand-bbrv3/receive_cpu | 1.044 | 1.040 | 1.029 | 1.053 |
| S5/stream | cand-bbrv3/send_rss | 1.217 | 1.235 | 1.217 | 1.192 |
| S5/stream | cand-bbrv3/receive_rss | 1.323 | 1.518 | 1.482 | 1.417 |
| S5/stream | cand-bbrv3/control_p95 | 0.874 | 1.061 | 1.096 | 1.037 |
| S5/stream | cand-reno/goodput | 1.000 | 1.000 | 1.000 | 1.000 |
| S5/stream | cand-reno/send_cpu | 0.958 | 1.003 | 1.002 | 1.004 |
| S5/stream | cand-reno/receive_cpu | 1.006 | 0.991 | 0.997 | 0.995 |
| S5/stream | cand-reno/send_rss | 1.004 | 0.999 | 0.992 | 1.004 |
| S5/stream | cand-reno/receive_rss | 1.041 | 0.988 | 0.974 | 1.006 |
| S5/stream | cand-reno/control_p95 | 0.981 | 0.993 | 0.997 | 1.039 |
| S5/datagram | cand-bbrv3/goodput | 0.971 | 0.972 | 0.972 | 0.972 |
| S5/datagram | cand-bbrv3/send_cpu | 0.962 | 0.953 | 0.948 | 0.965 |
| S5/datagram | cand-bbrv3/receive_cpu | 1.011 | 0.988 | 0.992 | 1.005 |
| S5/datagram | cand-bbrv3/send_rss | 1.122 | 1.159 | 1.157 | 1.181 |
| S5/datagram | cand-bbrv3/receive_rss | 1.068 | 1.070 | 1.072 | 1.058 |
| S5/datagram | cand-bbrv3/control_p95 | 0.984 | 1.021 | 1.037 | 1.000 |
| S5/datagram | cand-reno/goodput | 1.000 | 1.000 | 1.000 | 1.000 |
| S5/datagram | cand-reno/send_cpu | 0.959 | 0.993 | 0.995 | 1.003 |
| S5/datagram | cand-reno/receive_cpu | 1.006 | 0.999 | 0.999 | 1.005 |
| S5/datagram | cand-reno/send_rss | 0.999 | 1.023 | 1.006 | 1.157 |
| S5/datagram | cand-reno/receive_rss | 0.997 | 0.990 | 1.008 | 1.001 |
| S5/datagram | cand-reno/control_p95 | 0.980 | 1.003 | 1.005 | 1.047 |
| S6/stream | cand-bbrv3/goodput | 14.292 | 14.534 | 14.284 | 14.138 |
| S6/stream | cand-bbrv3/send_cpu | 0.087 | 0.086 | 0.089 | 0.087 |
| S6/stream | cand-bbrv3/receive_cpu | 0.073 | 0.073 | 0.073 | 0.075 |
| S6/stream | cand-bbrv3/send_rss | 1.680 | 1.673 | 1.656 | 1.640 |
| S6/stream | cand-bbrv3/receive_rss | 1.481 | 1.898 | 1.832 | 1.481 |
| S6/stream | cand-bbrv3/control_p95 | 1.462 | 1.562 | 1.581 | 1.414 |
| S6/datagram | cand-bbrv3/goodput | 13.926 | 14.744 | 14.423 | 13.743 |
| S6/datagram | cand-bbrv3/send_cpu | 0.092 | 0.090 | 0.088 | 0.093 |
| S6/datagram | cand-bbrv3/receive_cpu | 0.080 | 0.078 | 0.076 | 0.081 |
| S6/datagram | cand-bbrv3/send_rss | 1.498 | 1.542 | 1.466 | 1.481 |
| S6/datagram | cand-bbrv3/receive_rss | 1.126 | 1.095 | 1.090 | 1.100 |
| S6/datagram | cand-bbrv3/control_p95 | 1.358 | 1.459 | 1.461 | 1.415 |

## Timeline, preservation and loss

These raw-derived structures retain the registered classifications and their supporting values. Follow-up outcomes never automatically clear a raised readiness cell.

```json
{
  "cpu_split": {
    "S5/datagram/receive": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.19630028789261947,
      "registered": false,
      "window_ratio": 1.005716734767826
    },
    "S5/datagram/send": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.0,
      "registered": false,
      "window_ratio": 1.0013169704170235
    },
    "S5/stream/receive": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.2731544425857198,
      "registered": false,
      "window_ratio": 1.0530293552426435
    },
    "S5/stream/send": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.0,
      "registered": false,
      "window_ratio": 0.9459468148039266
    },
    "S6/datagram/receive": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.0,
      "registered": false,
      "window_ratio": 0.08202750695550602
    },
    "S6/datagram/send": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.0,
      "registered": false,
      "window_ratio": 0.10040181606484308
    },
    "S6/stream/receive": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.0,
      "registered": false,
      "window_ratio": 0.07380132311148167
    },
    "S6/stream/send": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.0,
      "registered": false,
      "window_ratio": 0.08906453547877917
    },
    "loopback/datagram/receive": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.469376714030163,
      "registered": false,
      "window_ratio": 1.0146303412296729
    },
    "loopback/datagram/send": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.22135949998845827,
      "registered": false,
      "window_ratio": 1.0927343412450825
    },
    "loopback/stream/receive": {
      "flag_raised": false,
      "outcome": "mixed",
      "outside_share": 0.22921013039833468,
      "registered": false,
      "window_ratio": 1.0786665212212043
    },
    "loopback/stream/send": {
      "flag_raised": true,
      "outcome": "inside window",
      "outside_share": 0.21815410767217275,
      "registered": false,
      "window_ratio": 1.1135761171919083
    }
  },
  "preslat": {
    "loopback/datagram": {
      "blocks": [
        {
          "block": 1,
          "counted": "original",
          "stage": "preslat-datagram",
          "status": "clean",
          "workload": "datagram"
        },
        {
          "block": 2,
          "counted": "original",
          "stage": "preslat-datagram",
          "status": "clean",
          "workload": "datagram"
        },
        {
          "block": 3,
          "counted": "original",
          "stage": "preslat-datagram",
          "status": "clean",
          "workload": "datagram"
        },
        {
          "block": 4,
          "counted": "original",
          "stage": "preslat-datagram",
          "status": "clean",
          "workload": "datagram"
        },
        {
          "block": 5,
          "counted": "original",
          "stage": "preslat-datagram",
          "status": "clean",
          "workload": "datagram"
        },
        {
          "block": 6,
          "counted": "original",
          "stage": "preslat-datagram",
          "status": "clean",
          "workload": "datagram"
        }
      ],
      "detail": {
        "blocks": 6,
        "cand_p50_ms": 0.15765,
        "cand_p95_ms": 0.767539,
        "ci": [
          0.6583010133379104,
          1.1272692073820376
        ],
        "per_block": [
          1.3046666265988427,
          1.277574285255922,
          0.43199024196059016,
          1.1297029853238023,
          0.8967379157772675,
          0.41851049010226465
        ],
        "ratio": 0.8337232896234695,
        "reno_p50_ms": 0.1562325,
        "reno_p95_ms": 0.920616
      },
      "outcome": "not reproduced"
    }
  },
  "presrss": {},
  "s5timeline": {
    "instrumented_over_readiness_goodput": {
      "datagram": 1.0069771765588904,
      "stream": 0.9981125040779234
    },
    "observations": [
      {
        "block": 1,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.027299682666666603,
          "idle": 0.029727637333333334,
          "loss": 0.0,
          "model": 0.018104013539983362,
          "queue_missing": 0.0,
          "timing": 9.217812040274352e-05,
          "unexplained": 0.011531445672947227
        },
        "goodput_mbps": 93.4241792,
        "id": "s5timeline-S5-stream-p1-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "stream"
      },
      {
        "block": 2,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.02918185333333323,
          "idle": 0.03183456266666666,
          "loss": 0.0,
          "model": 0.019300689503545077,
          "queue_missing": 0.0,
          "timing": 8.56940989750886e-05,
          "unexplained": 0.0124481790641465
        },
        "goodput_mbps": 93.09245440000001,
        "id": "s5timeline-S5-stream-p2-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "stream"
      },
      {
        "block": 3,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.02465969866666662,
          "idle": 0.028264544000000003,
          "loss": 0.0,
          "model": 0.014864463003496226,
          "queue_missing": 0.0,
          "timing": 0.00010715888530913631,
          "unexplained": 0.01329292211119464
        },
        "goodput_mbps": 93.54639359999999,
        "id": "s5timeline-S5-stream-p3-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "stream"
      },
      {
        "block": 4,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.02757975200000007,
          "idle": 0.026622634666666666,
          "loss": 0.0,
          "model": 0.014557053744389674,
          "queue_missing": 0.0,
          "timing": 0.00010672765499948531,
          "unexplained": 0.011958853267277504
        },
        "goodput_mbps": 93.5332992,
        "id": "s5timeline-S5-stream-p4-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "stream"
      },
      {
        "block": 1,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.02098040533333334,
          "idle": 0.018381477333333333,
          "loss": 0.0,
          "model": 0.009730475913280148,
          "queue_missing": 0.0,
          "timing": 0.00015183149918460457,
          "unexplained": 0.008499169920868582
        },
        "goodput_mbps": 92.61374293333334,
        "id": "s5timeline-S5-datagram-p1-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "datagram"
      },
      {
        "block": 2,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.02655163199999999,
          "idle": 0.023515874666666665,
          "loss": 0.0,
          "model": 0.014974159094288642,
          "queue_missing": 0.0,
          "timing": 9.722427017519698e-05,
          "unexplained": 0.008444491302202827
        },
        "goodput_mbps": 92.14866773333334,
        "id": "s5timeline-S5-datagram-p2-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "datagram"
      },
      {
        "block": 3,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.01992640000000001,
          "idle": 0.01930507733333333,
          "loss": 0.0,
          "model": 0.0100176510485002,
          "queue_missing": 0.0,
          "timing": 0.00012858887293626283,
          "unexplained": 0.009158837411896869
        },
        "goodput_mbps": 92.53859840000001,
        "id": "s5timeline-S5-datagram-p3-cand-timeline-bbrv3",
        "verdict": "no deficit",
        "workload": "datagram"
      },
      {
        "block": 4,
        "decomposition": {
          "coverage_s": 30.0,
          "deficit": 0.02930650933333334,
          "idle": 0.028293719999999994,
          "loss": 0.0,
          "model": 0.01890680463170491,
          "queue_missing": 0.0,
          "timing": 0.00012763245122797713,
          "unexplained": 0.009259282917067109
        },
        "goodput_mbps": 91.64507306666667,
        "id": "s5timeline-S5-datagram-p4-cand-timeline-bbrv3",
        "verdict": "model",
        "workload": "datagram"
      }
    ],
    "verdict": "model behaviour",
    "workload_counts": {
      "datagram": {
        "model": 3,
        "no deficit": 1
      },
      "stream": {
        "model": 4
      }
    },
    "workloads": {
      "datagram": "model",
      "stream": "model"
    }
  }
}
```

DATAGRAM loss is descriptive:

```json
{
  "S5/cand-bbrv3": {
    "blocks": 5,
    "loss_max": 0.00027503110470827055,
    "loss_median": 0.00025446812267440806,
    "lost_median": 98,
    "lost_minus_relay_dropped_median": 0
  },
  "S5/cand-reno": {
    "blocks": 5,
    "loss_max": 0.004404942793604154,
    "loss_median": 0.004087262037792073,
    "lost_median": 1624,
    "lost_minus_relay_dropped_median": 0
  },
  "S5/old-reno": {
    "blocks": 5,
    "loss_max": 0.0043924417800206795,
    "loss_median": 0.004214960467445406,
    "lost_median": 1675,
    "lost_minus_relay_dropped_median": 0
  },
  "S5/reno-reno": {
    "blocks": 5,
    "loss_max": 0.004012121944343764,
    "loss_median": 0.003939376395458012,
    "lost_median": 1565,
    "lost_minus_relay_dropped_median": 0
  },
  "S5/reno-reno-aa": {
    "blocks": 5,
    "loss_max": 0.004344982011220973,
    "loss_median": 0.00403467352619895,
    "lost_median": 1603,
    "lost_minus_relay_dropped_median": 0
  },
  "S6/cand-bbrv3": {
    "blocks": 5,
    "loss_max": 0.002290327942282609,
    "loss_median": 0.001334060494189705,
    "lost_median": 477,
    "lost_minus_relay_dropped_median": -1
  },
  "S6/old-reno": {
    "blocks": 5,
    "loss_max": 0.0011238059561715678,
    "loss_median": 0.0009214354995150339,
    "lost_median": 30,
    "lost_minus_relay_dropped_median": 0
  },
  "S6/reno-reno": {
    "blocks": 5,
    "loss_max": 0.0011282437006393381,
    "loss_median": 0.0009563746045758847,
    "lost_median": 30,
    "lost_minus_relay_dropped_median": -1
  },
  "S6/reno-reno-aa": {
    "blocks": 5,
    "loss_max": 0.001120615591498263,
    "loss_median": 0.0009559057820044609,
    "lost_median": 30,
    "lost_minus_relay_dropped_median": 0
  },
  "loopback/cand-bbrv3": {
    "blocks": 5,
    "loss_max": 0.0,
    "loss_median": 0.0,
    "lost_median": 0,
    "lost_minus_relay_dropped_median": null
  },
  "loopback/cand-reno": {
    "blocks": 5,
    "loss_max": 7.022558726330228e-05,
    "loss_median": 0.0,
    "lost_median": 0,
    "lost_minus_relay_dropped_median": null
  },
  "loopback/old-reno": {
    "blocks": 5,
    "loss_max": 0.0006067417161506953,
    "loss_median": 0.0004943062128617252,
    "lost_median": 6055,
    "lost_minus_relay_dropped_median": null
  },
  "loopback/reno-reno": {
    "blocks": 5,
    "loss_max": 3.927959477424271e-06,
    "loss_median": 8.01235417606449e-07,
    "lost_median": 11,
    "lost_minus_relay_dropped_median": null
  },
  "loopback/reno-reno-aa": {
    "blocks": 5,
    "loss_max": 5.2652260313068725e-05,
    "loss_median": 6.587771997211176e-07,
    "lost_median": 9,
    "lost_minus_relay_dropped_median": null
  }
}
```
