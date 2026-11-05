import importlib.util
import sys
sys.dont_write_bytecode = True
from pathlib import Path
import collections
p=Path(__file__).with_name('analyze-kernel.py')
s=importlib.util.spec_from_file_location('ka',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
def row(*a):return tuple(a)+(0,)*(16-len(a))
def data(rows):
 d=collections.defaultdict(list)
 for r in rows:d[r[0]].append(r)
 return {'rows':d,'counts':dict(collections.Counter(r[0] for r in rows)),'header':{'dropped':0}}
S=3_000_000;R=100_000_000;W=9_000_000_000
send=[row(30,1,1,1,1,0,0,1),row(50,S+2000,7,16,1126,S,-1,0),row(51,S+260000,99,32,38,S+255000),row(52,S+261000,99,32,7,S+255000,0),row(31,S+255000,32,W+S+220000,S+220000,S+250000,W+S+250000,1,1,0,38,1,1,1,0,0),row(32,S+25000,1,16,S+5000,W+S+5000,W+S+25000,1126,0,1126,0,1,1),row(33,S+30000,1,1,W+S+10000,S+30000,W+S+30000,42,4,0,0,0,1,1,0),row(33,S+31000,1,0,W+S+20000,S+31000,W+S+31000,42,4,0,0,0,1,1,0)]
receive=[row(30,1,1,2,1,0,0,2,1,1,0,1),row(31,R+85000,16,R,R,R+80000,1230000000000,1,1,0,1126,2,1,1,0,0),row(51,R+90000,7,16,1126,R+85000),row(50,R+95000,99,32,38,R+94000,7,0),row(8,R+105000,99,R+100000,38,1)]
base={'probes':[{'index':0,'start_mono_ns':0,'end_mono_ns':200_000_000,'start_schedule_s':0,'duration_ms':200,'complete':True}]}
out=m.analyze_rows({'send':data(send),'receive':data(receive)},base,1)
assert out['sample_counts']=={'sender_rx':1,'receiver_rx':1,'sender_tx':1,'complete_cycles':1,'partial_cycles':1},out['sample_counts']
c=out['correlated_cycles'][0]
assert c['raw_rtt_us']==255
assert c['sender_logical_send_to_software_tx_us']==20
assert c['receiver_socket_delivery_to_ack_write_start_us']==100
assert c['sender_kernel_rx_to_protocol_receive_us']==35
assert c['residual_after_measured_local_durations_us']==100,c
assert c['receiver_socket_to_batch_return_us']==80
assert c['sender_kernel_rx_to_batch_return_us']==30
assert out['correlated_partial_cycles'][0]['remaining_including_sender_logical_send_to_tx_us']==120
# Missing Linux TX timestamps preserve an honestly partial matched cycle.
without_tx=[r for r in send if r[0] not in (32,33)]
partial=m.analyze_rows({'send':data(without_tx),'receive':data(receive)},base,1)
assert partial['sample_counts']['complete_cycles']==0
assert partial['sample_counts']['partial_cycles']==1
assert partial['correlated_partial_cycles'][0]['remaining_including_sender_logical_send_to_tx_us']==120
assert partial['probes'][0]['sample_counts']['partial_cycles']==1
# Duplicating a matching receiver identity excludes that cycle rather than selecting one.
out2=m.analyze_rows({'send':data(send),'receive':data(receive+[receive[2]])},base,1)
assert out2['sample_counts']['complete_cycles']==0
# Aggregated GRO uses a shared stamp and must not be interpreted per packet.
aggregate=list(receive[1]);aggregate[10]=2252;aggregate[15]=1126
out3=m.analyze_rows({'send':data(send),'receive':data([receive[0],tuple(aggregate),*receive[2:]])},base,1)
assert out3['sample_counts']['complete_cycles']==0
# Platform MSG_CTRUNC flags differ and must both exclude the cycle.
for role, flag in [('receive', 0x20), ('send', 0x8)]:
 changed = [list(r) for r in (receive if role == 'receive' else send)]
 for r in changed:
  if r[0] == 31:r[9] |= flag
 dataset = {'send': data(send), 'receive': data(receive)}
 dataset[role] = data([tuple(r) for r in changed])
 assert m.analyze_rows(dataset,base,1)['sample_counts']['complete_cycles'] == 0
# A unique PN must not bypass an ambiguous identity, or vice versa.
collision = list(send[1]);collision[2]=8
assert m.analyze_rows({'send':data(send+[tuple(collision)]),'receive':data(receive)},base,1)['sample_counts']['complete_cycles']==0
collision = list(send[5]);collision[3]=48
assert m.analyze_rows({'send':data(send+[tuple(collision)]),'receive':data(receive)},base,1)['sample_counts']['complete_cycles']==0
# Stored converted time is redundant evidence and must agree with raw+anchor.
changed=[list(r) for r in send]
for r in changed:
 if r[0]==31:r[4]+=1_000_000
assert m.analyze_rows({'send':data([tuple(r) for r in changed]),'receive':data(receive)},base,1)['sample_counts']['complete_cycles']==0
print('Complete-cycle witness passed: distinct endpoint clocks, 255=20+100+35+100 us, bidirectional ambiguity, GRO/CTRUNC and clock-conversion mismatch exclusion, honest partial cycle without Linux TX.')
