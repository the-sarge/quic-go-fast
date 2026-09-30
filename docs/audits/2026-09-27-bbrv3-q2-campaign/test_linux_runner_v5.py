"""Native SSH transport reproduction; no cloud or comparative traffic."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import shlex
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('cloud5', HERE / 'run-linux-v5.py')
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)


class StagingTests(unittest.TestCase):
    def test_existing_ssh_stream_survives_stalled_separate_scp_tunnel(self):
        with tempfile.TemporaryDirectory(prefix='q2-native-ssh-') as temp:
            root = Path(temp)
            for key in ('host', 'client'):
                subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(root / key)], check=True)
            (root / 'authorized_keys').write_text((root / 'client.pub').read_text())
            listener = socket.socket()
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
            listener.close()
            user = pwd.getpwuid(os.getuid()).pw_name
            (root / 'known_hosts').write_text(f'[127.0.0.1]:{port} ' + (root / 'host.pub').read_text())
            (root / 'sshd.conf').write_text(f'Port {port}\nListenAddress 127.0.0.1\nHostKey {root}/host\nPidFile {root}/sshd.pid\nAuthorizedKeysFile {root}/authorized_keys\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nStrictModes no\nAllowUsers {user}\nLogLevel VERBOSE\n')
            # The first tunnel works. A new independent tunnel stalls, matching
            # the observed working SSH / silent timed-out SCP boundary.
            proxy = root / 'proxy.py'
            proxy.write_text('import os,pathlib,select,socket,sys,time\n'
                             'p=pathlib.Path(__file__).parent\n'
                             'if (p/"tunnel-open").exists():\n'
                             ' (p/"second-tunnel").write_text("stalled")\n'
                             ' time.sleep(60)\n'
                             ' raise SystemExit(1)\n'
                             '(p/"tunnel-open").write_text("ready")\n'
                             f's=socket.create_connection(("127.0.0.1",{port}))\n'
                             'inputs=[s,0]\n'
                             'while inputs:\n'
                             ' for x in select.select(inputs,[],[])[0]:\n'
                             '  if x==0:\n'
                             '   data=os.read(0,65536)\n'
                             '   if data:s.sendall(data)\n'
                             '   else:inputs.remove(0);s.shutdown(socket.SHUT_WR)\n'
                             '  else:\n'
                             '   data=s.recv(65536)\n'
                             '   if not data:raise SystemExit(0)\n'
                             '   os.write(1,data)\n')
            options = ['-F', '/dev/null', '-i', str(root / 'client'),
                       '-oIdentitiesOnly=yes', '-oIdentityAgent=none', '-oBatchMode=yes',
                       '-oStrictHostKeyChecking=yes', '-oUserKnownHostsFile=' + str(root / 'known_hosts'),
                       '-oControlMaster=auto', '-oControlPath=' + str(root / 'control'), '-oControlPersist=30',
                       '-oProxyCommand=' + shlex.join([sys.executable, str(proxy)])]
            host = user + '@127.0.0.1'
            ssh = ['ssh'] + options + ['-p', str(port)]
            payload = root / 'endpoints.tar.gz'
            payload.write_bytes(bytes(range(256)) * 25218)  # 6.46 MB binary transfer.
            adapter = root / 'adapter.py'
            adapter.write_text('import os,sys\nargs=' + repr(ssh) + '\nhost=sys.argv[1]\ncommand=sys.argv[2].removeprefix("--command=")\nos.execvp(args[0],args+[host,command])\n')
            log = (root / 'sshd.log').open('w')
            server = subprocess.Popen(['/usr/sbin/sshd', '-D', '-e', '-f', str(root / 'sshd.conf')], stdout=log, stderr=log)
            scp = None
            try:
                time.sleep(.2)
                warm = subprocess.run(ssh + [host, 'printf ready'], capture_output=True, text=True, timeout=5)
                self.assertEqual(warm.returncode, 0, warm.stderr)
                self.assertEqual(warm.stdout, 'ready')
                scp = subprocess.Popen(['scp', '-oControlMaster=no', '-oControlPath=none'] + options + ['-O', '-P', str(port), str(payload), host + ':' + str(root / 'old-upload')], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
                with self.assertRaises(subprocess.TimeoutExpired):
                    scp.wait(timeout=.75)
                self.assertTrue((root / 'second-tunnel').exists())
                os.killpg(scp.pid, signal.SIGTERM)
                scp.wait(timeout=3)
                self.assertEqual(subprocess.run(ssh + [host, 'printf still-ready'], capture_output=True, text=True, timeout=3).stdout, 'still-ready')
                destination = root / 'remote'
                destination.mkdir()
                with patch.object(cloud, 'ROOT', root), patch.object(cloud, 'STAGE', str(destination)), patch.object(cloud, 'SSH', [sys.executable, str(adapter)]):
                    cloud.stage_archive(host, payload.name, bound=5)
                self.assertEqual(hashlib.sha256((destination / payload.name).read_bytes()).digest(), hashlib.sha256(payload.read_bytes()).digest())
                self.assertEqual(json.loads((root / (host + '-stage-exit.json')).read_text())['exit_code'], 0)
            finally:
                if scp and scp.poll() is None:
                    os.killpg(scp.pid, signal.SIGKILL)
                    scp.wait(timeout=3)
                subprocess.run(ssh + ['-O', 'exit', host], capture_output=True, timeout=3)
                server.terminate()
                server.wait(timeout=3)
                log.close()

    def test_stream_timeout_stays_failed_and_does_not_retry(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'archive').write_bytes(b'payload')
            sink = root / 'sink.py'
            sink.write_text('import time\ntime.sleep(60)\n')
            with patch.object(cloud, 'ROOT', root), patch.object(cloud, 'STAGE', '/tmp/fresh'), patch.object(cloud, 'SSH', [sys.executable, str(sink)]):
                with self.assertRaisesRegex(RuntimeError, 'host-stage: exit124'):
                    cloud.stage_archive('host', 'archive', bound=.1)
            self.assertEqual(json.loads((root / 'host-stage-exit.json').read_text())['exit_code'], 124)
            self.assertEqual(len(list(root.glob('host-stage-exit.json'))), 1)


if __name__ == '__main__':
    unittest.main()
