"""Offline analysis of issue #1210 diagnostic artifacts; no qualification verdict."""
import argparse
import bisect
import collections
import datetime
import gzip
import tempfile
import json
import mmap
from pathlib import Path
import statistics
import struct
import tarfile


def stats(values):
    values = sorted(values)
    if not values:
        return {'n': 0}
    return dict(n=len(values), mean=statistics.fmean(values), p50=statistics.median(values), p95=values[int(.95*(len(values)-1))], p99=values[int(.99*(len(values)-1))], min=values[0], max=values[-1])


def records(path):
    with path.open('rb') as f:
        with mmap.mmap(f.fileno(), 0, access=mmap.ACCESS_READ) as data:
            assert len(data) % 128 == 0
            yield from struct.iter_unpack('<16q', data)


def ledger(path, payload, rate):
    data = path.read_bytes() if path.exists() else gzip.decompress(path.with_suffix(path.suffix+'.gz').read_bytes())
    assert data[:8] == b'WMLDG001'
    count = struct.unpack_from('<Q', data, 8)[0]
    raw = data[32:]
    bins = collections.defaultdict(collections.Counter)
    for i in range(count):
        byte, shift = divmod(i*3, 8)
        value = raw[byte] | ((raw[byte+1] << 8) if shift > 5 else 0)
        outcome = (value >> shift) & 3
        bins[int(i*payload*8/rate/.01)][outcome] += 1
    return [dict(start_s=k/100, slots=sum(v.values()), missed=v[0], attempted=sum(v.values())-v[0], admitted=v[1], rejected=v[2], canceled=v[3]) for k, v in sorted(bins.items())]


def header(log):
    result = [json.loads(x.split('[DEBUG-1210] ', 1)[1]) for x in log.read_text().splitlines() if x.startswith('[DEBUG-1210] {')]
    assert len(result) == 1, (log, result)
    return result[0]


def unix_ns(iso):
    # Preserve nanoseconds beyond datetime's microsecond precision.
    head, fraction = iso.replace('Z', '+00:00').split('.', 1)
    nanos, tz = fraction.split('+', 1)
    return int(datetime.datetime.fromisoformat(head+'+'+tz).timestamp())*10**9 + int(nanos.ljust(9, '0'))


def _analyze(root, arm, scratch):
    result_path = root/'controller'/arm/'result.json' if arm else next(root.glob('controller/*/result.json'))
    directory = result_path.parent
    result = json.loads(result_path.read_text())
    bins = ledger(directory/'send-datagram-ledger.bin', result['config']['payload_bytes'], result['config']['rate_bps'])
    ledger_path = directory/'send-datagram-ledger.bin'
    ledger_data = ledger_path.read_bytes() if ledger_path.exists() else gzip.decompress(ledger_path.with_suffix('.bin.gz').read_bytes())
    slot_count = struct.unpack_from('<Q', ledger_data, 8)[0]
    slot_step_ns = result['config']['payload_bytes']*8*10**9/result['config']['rate_bps']
    output = dict(root=str(root), status=result['status'], summary=result['summary'], window_alignment_qualified=result['remote']['window_alignment_qualified'], bins=bins)
    reports = [json.loads(x) for x in (directory/'send-stderr.log').read_text().splitlines() if x.startswith('{') and 'diag1460' in x[:400]]
    output['historical_observer'] = reports
    if '[DEBUG-1210]' not in (directory/'send-stderr.log').read_text():
        return output
    sender_header, receiver_header = header(directory/'send-stderr.log'), header(directory/'receive-stderr.log')
    output['headers'] = dict(send=sender_header, receive=receiver_header)
    # Read verified archived remote bytes without changing historical artifacts.
    sender_name = Path(sender_header['diag1210']).name
    sender_path = scratch/sender_name
    if (root/(sender_name+'.gz')).exists():
        sender_path.write_bytes(gzip.decompress((root/(sender_name+'.gz')).read_bytes()))
    else:
        with tarfile.open(root/'remote-worker-files.tar.gz') as archive:
            member = next(x for x in archive.getmembers() if Path(x.name).name == sender_name)
            sender_path.write_bytes(archive.extractfile(member).read())
    receiver_path = root/'local'/Path(receiver_header['diag1210']).name
    if not receiver_path.exists():
        packed = receiver_path.with_suffix('.bin.gz')
        receiver_path = scratch/('receive-'+receiver_path.name)
        receiver_path.write_bytes(gzip.decompress(packed.read_bytes()))
    sender = collections.defaultdict(list)
    for row in records(sender_path):
        sender[row[0]].append(row)
    for rows in sender.values():
        rows.sort(key=lambda x: x[1])
    output['record_counts_send'] = {str(k): len(v) for k, v in sender.items()}
    # Retain only receiver records matching ACKs sampled on the data sender.
    ack_numbers = {r[2] for r in sender[3]}
    rx_ack, links, rx_data = {}, {}, {}
    rx_regs, rx_handoffs, rx_writes = {}, {}, {}
    rx_count = collections.Counter()
    for row in records(receiver_path):
        kind, pn = row[0], row[2]
        rx_count[kind] += 1
        if kind == 2 and pn in ack_numbers:
            rx_ack[pn] = row
        elif kind == 12 and row[3] in ack_numbers:
            links[row[3]] = row
        elif kind == 9:
            rx_data[pn] = row
        elif kind == 1:
            rx_regs[pn] = row
        elif kind == 7:
            rx_handoffs[pn] = row
        elif kind == 8:
            rx_writes[pn] = row
    output['record_counts_receive'] = dict(rx_count)
    regs = {r[2]: r for r in sender[1]}
    handoffs = {r[2]: r for r in sender[7]}
    writes = {r[2]: r for r in sender[8]}
    acks = {r[2]: r for r in sender[3]}
    residence = {(r[2], r[3]): r for r in sender[14]}
    flights = sorted([(r[1], r[8], r[6]) for r in sender[4]] + [(r[1], r[5], r[6]) for r in sender[15]])
    flight_times = [r[0] for r in flights]
    measurement_peer = next(p for p in result['peers'] if p['role'] == 'send')
    anchor = sender_header['origin_mono_ns'] + unix_ns(measurement_peer['measurement_start_utc']) - sender_header['origin_unix_ns']
    output['measurement_mono_ns'] = anchor
    output['clock_anchor_drift_ns'] = {role: (h['end_unix_ns']-h['origin_unix_ns'])-(h['end_mono_ns']-h['origin_mono_ns']) for role, h in output['headers'].items()}
    probes, active = [], None
    for row in sender[4]:
        if row[5] == 6 and active is None:
            active = [row]
        elif active is not None:
            active.append(row)
            if row[5] != 6:
                probes.append(active)
                active = None
    if active:
        probes.append(active)
    intervals = []
    matched_rows = []
    for index, probe in enumerate(probes):
        start, end = probe[0][1], probe[-1][1]
        # Omit 2 ms at each boundary for steady-phase arithmetic, not qualification.
        lo, hi = start+2_000_000, end-2_000_000
        if hi <= lo:
            continue
        core = [r for r in probe if lo <= r[1] < hi]
        byte_count = sum(r[12] for r in core)
        raw_area = sum(residence[(r[2], r[3])][4] for r in core)
        process_area = sum(residence[(r[2], r[3])][5] for r in core)
        area, window_area = 0, 0
        first = max(0, bisect.bisect_right(flight_times, lo)-1)
        stop = bisect.bisect_left(flight_times, hi)
        for j in range(first, stop):
            left = max(lo, flights[j][0])
            right = min(hi, flights[j+1][0] if j+1 < len(flights) else hi)
            area += (right-left)*flights[j][1]
            window_area += (right-left)*flights[j][2]
        d = dict(index=index, start_mono_ns=start, end_mono_ns=end, start_schedule_s=(start-anchor)/1e9, duration_ms=(end-start)/1e6, complete=probe[-1][5] != 6, core_duration_ns=hi-lo, transport_mbps=byte_count*8000/(hi-lo), acknowledged_bytes=byte_count, byte_weighted_raw_us=raw_area/max(1, byte_count)/1000, byte_weighted_processed_us=process_area/max(1, byte_count)/1000, time_weighted_flight_bytes=area/(hi-lo), time_weighted_window_bytes=window_area/(hi-lo), raw_rtt_us=stats(r[9]/1000 for r in core if r[9]>0), minimum_rtt_us=stats(r[10]/1000 for r in core), ack_processing_us=stats((r[1]-r[3])/1000 for r in core), pending_at_feedback_bytes=stats(r[11] for r in core), lost_bytes=sum(residence[(r[2],r[3])][6] for r in core))
        # Integrate waiting after each observed emission stop until the next opportunity.
        starts = [r[1] for r in sender[5] if start <= r[1] <= end]
        waits = collections.Counter()
        work = 0
        for row in sender[6]:
            if not (lo <= row[1] < hi):
                continue
            nxt = bisect.bisect_right(starts, row[1])
            if nxt < len(starts):
                waits[row[3]] += min(hi, starts[nxt])-row[1]
            prev = bisect.bisect_right(starts, row[1])-1
            if prev >= 0:
                work += row[1]-max(lo, starts[prev])
        d['emission_wait_share_by_stop'] = {str(k): v/(hi-lo) for k, v in waits.items()}
        d['emission_work_share'] = work/(hi-lo)
        allowances = [r for r in sender[13] if lo <= r[1] < hi]
        d['allowance_bytes'] = stats(r[4] for r in allowances)
        d['pacing_credit_bytes'] = stats(r[6]/1e9 for r in allowances)
        d['packet_budget_bytes'] = stats(r[5] for r in allowances)
        all_starts = [r[1] for r in sender[5]]
        eligible_delays = []
        maximum_packet = d['packet_budget_bytes'].get('p50', 0)
        for feedback in core:
            next_opportunity = bisect.bisect_right(all_starts, feedback[1])
            if feedback[6]-feedback[8] >= maximum_packet and next_opportunity < len(all_starts):
                eligible_delays.append((all_starts[next_opportunity]-feedback[1])/1000)
        d['ack_feedback_to_next_eligible_send_opportunity_us'] = stats(eligible_delays)
        import math
        first_slot = max(0, math.ceil((lo-anchor)/slot_step_ns))
        last_slot = min(slot_count, math.ceil((hi-anchor)/slot_step_ns))
        if last_slot > first_slot:
            outcomes = collections.Counter()
            for slot in range(first_slot, last_slot):
                byte, shift = divmod(slot*3, 8)
                value = ledger_data[32+byte] | ((ledger_data[33+byte]<<8) if shift>5 else 0)
                outcomes[(value>>shift)&3] += 1
            d['scheduled_slot_cohort'] = dict(first=first_slot, end_exclusive=last_slot, outcomes=dict(outcomes), attempted_mbps=(last_slot-first_slot-outcomes[0])*result['config']['payload_bytes']*8000/(hi-lo))

        endpoints = collections.defaultdict(list)
        for row in core:
            pn = row[2]
            ack, reg, write, handoff = acks.get(pn), regs.get(pn), writes.get(pn), handoffs.get(pn)
            peer_ack, link, received = rx_ack.get(pn), links.get(pn), rx_data.get(pn)
            if not all((ack, reg, write, handoff, peer_ack, link)):
                continue
            peer_write = rx_writes.get(link[2])
            if not peer_write:
                continue
            raw = ack[3]-reg[3]
            send_local = write[3]-reg[3]
            peer_service = peer_write[3]-peer_ack[4]
            values = dict(raw_us=raw/1000, sender_logical_sendtime_to_registration_hook_us=(reg[1]-reg[3])/1000, sender_registration_to_write_start_us=send_local/1000, sender_handoff_to_write_start_us=(write[3]-handoff[1])/1000, sender_write_call_us=(write[1]-write[3])/1000, receiver_read_to_ack_write_start_us=peer_service/1000, receiver_ack_delay_field_us=ack[4]/1000, receiver_read_to_ack_frame_us=(peer_ack[1]-peer_ack[4])/1000, receiver_ack_frame_to_write_start_us=(peer_write[3]-peer_ack[1])/1000, receiver_ack_write_call_us=(peer_write[1]-peer_write[3])/1000, sender_ack_read_to_handler_us=(ack[1]-ack[3])/1000, sender_ack_read_to_feedback_done_us=(row[1]-ack[3])/1000, outside_instrumented_service_us=(raw-send_local-peer_service)/1000, ack_group_packets=peer_ack[6])
            if received:
                values['receiver_read_to_packet_processing_us'] = (received[1]-received[3])/1000
            for key, value in values.items():
                endpoints[key].append(value)
            matched_rows.append(dict(probe=index, largest_acked=pn, sender_ack_mono_ns=ack[3], **values))
        d['matched_ack_components'] = {k: stats(v) for k, v in endpoints.items()}
        intervals.append(d)
    output['probes'] = intervals
    output['matched_acks'] = matched_rows
    return output


def analyze(root, arm=None):
    with tempfile.TemporaryDirectory(prefix='q1210-analysis-') as scratch:
        return _analyze(root, arm, Path(scratch))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('root', type=Path)
    parser.add_argument('output', type=Path)
    parser.add_argument('--arm')
    args = parser.parse_args()
    result = analyze(args.root, args.arm)
    args.output.write_text(json.dumps(result, indent=2)+'\n')
    print(json.dumps({k: v for k, v in result.items() if k not in ('bins','historical_observer','matched_acks')}, indent=2))
