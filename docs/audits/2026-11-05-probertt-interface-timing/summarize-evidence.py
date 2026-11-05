"""Derive report cohorts without changing measurements or substituting medians."""
import collections,importlib.util,json,sys
from pathlib import Path
sys.dont_write_bytecode=True
root=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('interface',root/'analyze-interface.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
output={}
for name in ['interface','napi','restored-interface']:
 d=json.loads((root/(name+'.json')).read_text());rows=d['correlated_cycles'];cohorts={}
 for size in sorted({r['bpf_data']['payload_length'] for r in rows}):
  subset=[r for r in rows if r['bpf_data']['payload_length']==size]
  cohorts[str(size)]={'n':len(subset),'aggregate':module.KERNEL.grouped_stats(subset),'probes':{str(p):{'n':sum(r['probe']==p for r in subset),'aggregate':module.KERNEL.grouped_stats([r for r in subset if r['probe']==p])} for p in sorted({r['probe'] for r in rows})}}
 residuals=[r['raw_rtt_ns']-r['sender_logical_send_to_software_tx_ns']-r['receiver_interface_turnaround_ns']-r['sender_kernel_rx_to_protocol_receive_ns']-r['residual_outside_receiver_interface_ns'] for r in rows]
 probes=[p for p in d['baseline_analysis']['probes'] if p['complete'] and p['start_schedule_s']>=0 and p['start_schedule_s']+p['duration_ms']/1000<=60]
 output[name]={'sample_counts':d['sample_counts'],'complete_payload_cohorts':cohorts,'partition_error_ns':module.KERNEL.stats(residuals),'gso_segment_counts':dict(collections.Counter(r['data_submission_segment_count'] for r in rows)),'clock_neighbor_max_span_ns':max((r.get('receiver_wall_mapping',{}).get('observed_neighbor_offset_span_ns',0) for r in rows),default=0),'probe_count':len(probes),'probe_metrics':[{'index':p['index'],'raw_us':p['byte_weighted_raw_us'],'minimum_us':p['minimum_rtt_us']['p50'],'transport_mbps':p['transport_mbps'],'window_bytes':p['time_weighted_window_bytes'],'flight_fraction':p['time_weighted_flight_bytes']/p['time_weighted_window_bytes']}for p in probes]}
(root/'cohorts.json').write_text(json.dumps(output,indent=2)+'\n')
for name,d in output.items():
 full=d['complete_payload_cohorts']['1126'];a=full['aggregate']
 print(name,'fullsize',full['n'],'means',[round(a[k]['mean'],6) for k in ['raw_rtt_us','sender_logical_send_to_software_tx_us','receiver_interface_turnaround_us','sender_kernel_rx_to_protocol_receive_us','residual_outside_receiver_interface_us']],'residual_median',a['residual_outside_receiver_interface_us']['p50'],'perprobe',[v['n']for v in full['probes'].values()])
