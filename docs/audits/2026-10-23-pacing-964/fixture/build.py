"""Build identical endpoint source against the two experiment worktrees."""
import hashlib,json,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
BUILD=ROOT/'build'
def main():
    BUILD.mkdir(exist_ok=True)
    source=(ROOT/'fixture/endpoint.go.src').read_bytes()
    (BUILD/'endpoint.go').write_bytes(source)
    manifest={}
    for arm in ['control','candidate']:
        wt=Path('/Volumes/worktrees/quic-go-fast')/f'pacing-964-{arm}'
        (BUILD/'go.mod').write_text(f'module assessment964\n\ngo 1.26.0\n\ntoolchain go1.27.3\n\nrequire github.com/quic-go/quic-go v0.0.0\n\nreplace github.com/quic-go/quic-go => {wt}\n')
        subprocess.run(['go','mod','tidy'],cwd=BUILD,check=True)
        subprocess.run(['go','build','-trimpath','-o',arm+'-endpoint','.'],cwd=BUILD,check=True)
        subprocess.run(['go','vet','./...'],cwd=BUILD,check=True)
        subprocess.run(['go','mod','tidy','-diff'],cwd=BUILD,check=True)
        manifest[arm]={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=wt,text=True).strip(),'binary_sha256':hashlib.sha256((BUILD/(arm+'-endpoint')).read_bytes()).hexdigest(),'module':(BUILD/'go.mod').read_text(),'go_sum':(BUILD/'go.sum').read_text()}
    (ROOT/'build-identities.json').write_text(json.dumps(manifest,indent=2)+'\n')
if __name__=='__main__':main()
