import json,math,random,re,sys
from pathlib import Path
p=Path(sys.argv[1]);manifest=json.loads((p/'manifest.json').read_text())
rows={}
for step in manifest['steps']:
 text=(p/f"{step['pair']:02}-{step['variant']}.log").read_text()
 for name,ns,bs,alloc in re.findall(r'BenchmarkQueueLifetime/(\w+)-4\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op',text):
  rows[(name,step['pair'],step['variant'])]=(float(ns),int(bs),int(alloc))
out={}
for name in ('HandoffDrain','CapacityPressure'):
 base=[rows[(name,i,'base')] for i in range(10)];cand=[rows[(name,i,'candidate')] for i in range(10)]
 ratios=[math.log(c[0]/b[0]) for b,c in zip(base,cand)]
 rng=random.Random(20260907)
 boots=sorted(math.exp(sum(rng.choice(ratios) for _ in ratios)/10) for _ in range(20000))
 out[name]=dict(base_ns=math.exp(sum(math.log(x[0]) for x in base)/10),candidate_ns=math.exp(sum(math.log(x[0]) for x in cand)/10),time_ratio=math.exp(sum(ratios)/10),upper95=boots[18999],base_allocs=[x[2] for x in base],candidate_allocs=[x[2] for x in cand],base_bytes=[x[1] for x in base],candidate_bytes=[x[1] for x in cand])
 out[name]['pass']=boots[18999]<=1.05 and all(c[2]<=b[2] for b,c in zip(base,cand))
print(json.dumps(out,indent=2))
