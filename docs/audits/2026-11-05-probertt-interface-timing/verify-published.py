"""Replay published numeric traces/packet subsets and compare exact correlations."""
import gzip,importlib.util,json,sys
from pathlib import Path
sys.dont_write_bytecode=True
root=Path(__file__).resolve().parent
arm=sys.argv[1];destination=Path(sys.argv[2]);destination.mkdir(parents=True,exist_ok=True)
def module(name,file):
 s=importlib.util.spec_from_file_location(name,root/file);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
m=module('interface','analyze-interface.py')
run='q1210-interface1' if arm=='interface' else 'q1210-napi1'
source=root/'observations'/run/arm
actual=m.analyze(source,[source/(arm+'-retained.pcap.gz')],arm)
expected=json.loads((root/(arm+'.json')).read_text())
def normalize(value):
 if isinstance(value,list):return [normalize(v)for v in value]
 if isinstance(value,dict):
  result={str(k):normalize(v) for k,v in value.items()}
  for name in ['bpf_data','bpf_ack']:
   if name in result:
    for key in ['capture','record']:result[name].pop(key,None)
  return result
 return value
keys=['arm','status','summary','sample_counts','aggregate','probes','exclusions','excluded_cycles','correlated_cycles','correlated_partial_cycles','correlated_turnaround_cycles','endpoint_quality','baseline_analysis']
for key in keys:
 if normalize(actual[key])!=normalize(expected[key]):
  (destination/('failure-'+arm+'.json')).write_text(json.dumps({'field':key,'actual':actual[key],'expected':expected[key]},indent=2)+'\n')
  raise AssertionError(key)
assert actual['capture_quality']['unique_needed_identities']==expected['capture_quality']['unique_needed_identities']
assert actual['capture_quality']['ambiguous_identities']==expected['capture_quality']['ambiguous_identities']
receipt={'arm':arm,'matched_fields':keys,'correlations_identical':True,'packet_inventory_expected_to_differ':True,'sample_counts':actual['sample_counts']}
if arm=='napi':
 n=module('napi','analyze-napi.py');raw=source/'linux-napi';trace=n.Trace({x['ack_identity']for x in actual['correlated_turnaround_cycles']})
 with gzip.open(raw/'stdout.log.gz','rt') as lines:trace.read(lines,'stdout')
 with (raw/'stderr.log').open() as lines:trace.read(lines,'stderr')
 observed=n.analyze(actual,trace);wanted=json.loads((root/'napi-correlations.json').read_text());wanted.pop('inputs')
 assert normalize(observed)==normalize(wanted),'NAPI correlations'
 receipt['napi_correlations_identical']=True
(destination/(arm+'-published-replay.json')).write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps(receipt),flush=True)
