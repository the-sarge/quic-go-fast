"""Replay the issue #1210 diagnostic identity format without editing its source."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

source = Path(sys.argv[1]).read_text()
program = source.replace('package diag1210', 'package main', 1).replace('import "os"', 'import ("os"; "fmt")\nvar Enabled = true', 1) + r'''
func main() {
    for i:=0;i<256;i++ {b:=[]byte{byte(i)};id:=Identity(b);fmt.Printf("%d %t\n",id,SelectedIdentity(id))}
    b:=make([]byte,1126);for i:=range b{b[i]=byte(i)};id:=Identity(b)
    b[35]^=255;if id!=Identity(b){panic("tail affected prefix identity")}
    b[0]^=255;if id==Identity(b){panic("prefix identity unchanged")}
    if Identity(b)==Identity(b[:1125]){panic("length missing")}
    if Identity(nil)!=0||SelectedIdentity(0){panic("empty selected")}
}
'''
expected = []
for i in range(256):
    value = 14695981039346656037
    for byte in (1).to_bytes(8, 'little') + bytes([i]):
        value = ((value ^ byte) * 1099511628211) & ((1 << 64) - 1)
    if value == 0:
        value = 1
    expected.append(value if value < (1 << 63) else value - (1 << 64))
with tempfile.TemporaryDirectory(prefix='q1210-identity-validation-') as directory:
    program_path = Path(directory) / 'main.go'
    program_path.write_text(program)
    for enabled in [False, True]:
        environment = os.environ.copy()
        environment['Q1210_KERNEL_TRACE'] = '1' if enabled else ''
        result = subprocess.check_output(['go', 'run', str(program_path)], text=True, env=environment)
        assert result.splitlines() == [f'{value} {str(enabled and value != 0 and value & 15 == 0).lower()}' for value in expected]
print(json.dumps({'identity_source_sha256': hashlib.sha256(source.encode()).hexdigest(), 'cross_language_vectors': 256, 'sampled_vectors': sum(value & 15 == 0 for value in expected), 'prefix_length_empty_and_gate_checks': 'pass'}))
