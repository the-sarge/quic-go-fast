#!/usr/bin/env python3
import hashlib,json,pathlib,struct,subprocess,uuid
kernel=pathlib.Path('/System/Library/Kernels/kernel.release.t6041')
out=pathlib.Path(__file__).parent
p=kernel.read_bytes(); base=0xfffffe0007004000; off=32; segs=[]; uuid_value=None; fixoff=None
for _ in range(struct.unpack_from('<I',p,16)[0]):
 cmd,size=struct.unpack_from('<II',p,off)
 if cmd==0x19:
  x=struct.unpack_from('<16sQQQQiiII',p,off+8); segs.append((x[0].rstrip(b'\0').decode(),x[1],x[3]))
 if cmd==0x1b: uuid_value=str(uuid.UUID(bytes=p[off+8:off+24])).upper()
 if cmd==0x80000034: fixoff=struct.unpack_from('<I',p,off+8)[0]
 off+=size
assert uuid_value=='D46FC75B-A889-3796-A66C-D28FDD50FB68'
assert hashlib.sha256(p).hexdigest()=='d0cf2fb69845bc5a34624ba69aa2b4f35ee65b4dbca150068a6cc93c5bc13bae'
starts=fixoff+struct.unpack_from('<I',p,fixoff+4)[0]
rel=struct.unpack_from('<I',p,starts+4+4)[0]
sz,page_size,fmt,seg_offset,max_ptr,page_count=struct.unpack_from('<IHHQIH',p,starts+rel)
assert fmt==7 # DYLD_CHAINED_PTR_ARM64E_KERNEL, stride 4.
entry=0x2276c8; page=(entry-seg_offset)//page_size
page_start=struct.unpack_from('<H',p,starts+rel+22+page*2)[0]
assert page_start<0x8000
chain=[];cur=seg_offset+page*page_size+page_start
while True:
 raw=struct.unpack_from('<Q',p,cur)[0]
 chain.append(cur)
 if cur==entry: break
 delta=(raw>>51)&0x7ff
 assert delta
 cur+=4*delta
assert raw>>63==1 and (raw>>62)&1==0
# ARM64E authenticated rebase target is the low 32-bit runtimeOffset.
assert base+(raw&0xffffffff)==0xfffffe00079b1544
records=[]
for i in [0,1,2,3,4,6,7,8,11,480,481,482]:
 o=0x2249b0+i*24;r,m,rt,n,b=struct.unpack_from('<QQIHH',p,o)
 records.append(dict(index=i,file_offset=hex(o),raw=hex(r),target=hex(base+(r&0xffffffff)),return_type=rt,narg=n,arg_bytes32=b))
mask=0x0100021000000021
result=dict(kernel=str(kernel),sha256=hashlib.sha256(p).hexdigest(),uuid=uuid_value,vm_base=hex(base),sysent_vm=hex(base+0x2249b0),entry_481=hex(entry),pointer_format=fmt,chain_page=page,chain_page_start=hex(page_start),chain_entry_count=len(chain),chain_contains_entry=True,entry_raw=hex(raw),suppression_mask=hex(mask),suppressed_errors=[i-1 for i in range(57) if mask&(1<<i)],sysent_records=records)
(out/'mapping.json').write_text(json.dumps(result,indent=2)+'\n')
for name,start,stop in [('sendmsg_x',0xfffffe00079b1544,0xfffffe00079b1818),('sendit',0xfffffe00079b0e00,0xfffffe00079b1110),('dispatch',0xfffffe0007aa7f28,0xfffffe0007aa8314)]:
 r=subprocess.run(['xcrun','llvm-objdump','-d',f'--start-address={start:#x}',f'--stop-address={stop:#x}',str(kernel)],check=True,capture_output=True,text=True)
 (out/(name+'.disassembly.txt')).write_text(r.stdout)
print(json.dumps(result,indent=2))
