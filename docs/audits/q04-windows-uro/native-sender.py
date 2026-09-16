import functools, http.server, socket, struct, threading
root = "/tmp/q04-windows-tools-20260916"
http = http.server.ThreadingHTTPServer(("192.168.122.1", 8504), functools.partial(http.server.SimpleHTTPRequestHandler, directory=root))
threading.Thread(target=http.serve_forever, daemon=True).start()
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("192.168.122.1", 8505))
s.settimeout(300)
print("Q04 sender ready at 192.168.122.1:8505", flush=True)
try:
 data, peer = s.recvfrom(1024)
 if data != b"Q04 URO": raise RuntimeError("unexpected trigger")
 payload = b"".join(bytes([i]) * (500 if i == 32 else 1232) for i in range(1, 33))
 sent = s.sendmsg([payload], [(socket.IPPROTO_UDP, 103, struct.pack("=H", 1232))], 0, peer)
 print(f"Sent {sent} bytes as 32 UDP segments to {peer}", flush=True)
finally:
 s.close()
 http.shutdown()
