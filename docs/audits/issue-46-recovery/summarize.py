"""Derive recovery-chain evidence from retained captures, without running tests."""
import json
from pathlib import Path

root = Path(__file__).resolve().parent
summary = {}
for case in ('C1', 'C2', 'C3', 'C4', 'C5'):
    rows = [json.loads(line) for line in (root/'raw'/f'{case}.jsonl').read_text().splitlines()]
    events = []
    for row in rows:
        if row['kind'] == 'qlog':
            event = row['data']
            assert 'encode_error' not in event
            events.append(dict(event, decoded=json.loads(event['json'])))
    routes = [r['data'] for r in rows if r['kind'] == 'route']
    assert sorted(r['order'] for r in routes) == list(range(1,len(routes)+1))
    selections = []
    for route in routes:
        if not route['selected']:
            continue
        checksum = route['crc_after']
        selections.append(dict(route,
            sender_writes=[r for r in rows if r['kind']=='socket_write' and r['data']['source']=='server' and r['data']['crc']==route['crc_before']],
            receiver_reads=[r for r in rows if r['kind']=='socket_read' and r['data']['source']=='client' and r['data']['crc']==checksum],
            receiver_events=[e for e in events if 'client=true' in e['source'] and e['decoded'].get('datagram_payload_checksum')==checksum],
        ))
    probes = []
    for event in events:
        payload = event['decoded']
        if event['event']!='recovery:loss_timer_updated' or payload.get('event_type')!='expired' or payload.get('timer_type')!='pto':
            continue
        sends = [e for e in events if e['event']=='transport:packet_sent' and e['source']==event['source'] and e['event_ns']>=event['event_ns']
                 and e['decoded']['header']['packet_type']==payload['packet_number_space']
                 and any(f['frame_type'] not in ('ack','padding','connection_close') for f in e['decoded'].get('frames',[]))]
        send = sends[0] if sends else None
        checksum = send['decoded']['datagram_payload_checksum'] if send else None
        source = 'client' if 'client=true' in event['source'] else 'server'
        probes.append({'expiry':event,'next_ack_eliciting_send':send,
                       'socket_writes':[r for r in rows if checksum is not None and r['kind']=='socket_write' and r['data']['source']==source and r['data']['crc']==checksum],
                       'routes':[r for r in routes if checksum is not None and r['crc_before']==checksum]})
    summary[case] = {
        'dial':next(r for r in rows if r['kind']=='dial_result'),
        'completion':next(r for r in rows if r['kind'] in ('transfer_complete','deadline_complete')),
        'record_count':len(rows), 'datagrams':len(routes), 'selections':selections, 'probe_chains':probes,
        'write_errors':[r for r in rows if r['kind']=='socket_write' and (r['data']['error'] or r['data']['n']!=r['data']['length'])],
        'handshake_receipts':[e for e in events if e['event']=='transport:packet_received' and e['decoded']['header']['packet_type']=='handshake'],
        'connection_closes':[e for e in events if e['event']=='transport:connection_closed'],
    }
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for case, value in summary.items():
    print(case, value['dial']['data'], 'selected',len(value['selections']), 'PTOs',len(value['probe_chains']), 'write errors',len(value['write_errors']))
