import copy
import unittest
import json
import subprocess
import sys
import tempfile
from pathlib import Path
import gridcast_receipt as gate


def receipts():
    start=10*gate.SECOND
    run=dict(id='test',controller='reno',workload='stream',gridcast_gate=True,start_unix_ns=start,warmup_ms=60000,measure_ms=60000)
    result=dict(run=run,controller='reno',receiver=dict(per_second_bytes=[1]*60,useful_bytes=60,corrupt=0,duplicates=0),control=dict(missed_opportunities=0,skipped=0,unresolved=0,offered=60,replies=60,latency_ns=[100]*60,opportunity_times_ns=[[(70+i)*gate.SECOND]*2 for i in range(60)],clock_offset_bounds_ns=[[-100,100]]))
    peer=dict(result=result)
    observation=dict(Error='',SocketDrops=0,NonCanonical=0,SendErrors=0,MissingTimestamps=0,Stats=dict(PropagationOverflow=0),MaxIngressLagNS=1,MaxEgressLagNS=1,Samples=[dict(UnixNS=i*gate.SECOND,FocalPackets=i,CompetitorPackets=i) for i in range(9,132)])
    router=dict(Config=dict(Scenario='gridcast-coexistence',Phase=0),Profiled=False,Observations=[observation,copy.deepcopy(observation)])
    app=dict(start_unix_ns=0,end_unix_ns=140*gate.SECOND,exit=0)
    return copy.deepcopy(peer),copy.deepcopy(peer),router,app


class ReconcileTests(unittest.TestCase):
    def test_fixed_window_reconciles(self):
        self.assertEqual(gate.reconcile(*receipts())['status'],'reconciled')

    def test_missing_overlap_and_invalid_adapter_cannot_pass(self):
        for mutation in ('flow','adapter','early','clock','delivery','latency','identity'):
            args=receipts();sender,receiver,router,app=args
            if mutation=='flow':router['Observations'][0]['Samples'][65]['FocalPackets']=64
            if mutation=='adapter':router['Observations'][0]['SocketDrops']=1
            if mutation=='early':app['end_unix_ns']=100*gate.SECOND
            if mutation=='clock':sender['result']['control']['clock_offset_bounds_ns']=[[100,0]]
            if mutation=='delivery':receiver['result']['receiver']['per_second_bytes'][3]=0
            if mutation=='latency':sender['result']['control']['skipped']=1
            if mutation=='identity':receiver['result']['run']['id']='different'
            with self.subTest(mutation=mutation):self.assertEqual(gate.reconcile(*args)['status'],'inconclusive')

    def test_zero_delivery_stall_remains_measured(self):
        args=receipts()
        for peer in args[:2]:peer['result']['receiver']['per_second_bytes'][:2]=[0,2]
        result=gate.reconcile(*args)
        self.assertEqual(result['status'],'reconciled')
        self.assertEqual(result['per_second_bytes'][:2],[0,2])

    def test_delayed_complete_control_and_censored_control(self):
        args=receipts();control=args[0]['result']['control']
        control.update(skipped=2,offered=58,replies=58,latency_ns=[3*gate.SECOND]+[100]*57)
        self.assertEqual(gate.reconcile(*args)['status'],'reconciled')
        control.update(unresolved=1,replies=57,latency_ns=[100]*57)
        self.assertEqual(gate.reconcile(*args)['status'],'inconclusive')

    def test_linux_competitor_does_not_need_unscheduled_traffic(self):
        for phase in (25_000_000,100_000_000,975_000_000):
            args=receipts()
            args[0]['result']['control']['clock_offset_bounds_ns']=[[-50_000_000,50_000_000]]
            for obs in args[2]['Observations']:
                obs['Samples']=[dict(UnixNS=i*gate.SECOND-phase,FocalPackets=i,CompetitorPackets=max(0,min(120*gate.SECOND,(i-10)*gate.SECOND-phase))) for i in range(8,134)]
            with self.subTest(phase=phase):self.assertEqual(gate.reconcile(*args)['status'],'reconciled')
            args[2]['Observations'][0]['Samples'][60]['CompetitorPackets']=args[2]['Observations'][0]['Samples'][59]['CompetitorPackets']
            self.assertEqual(gate.reconcile(*args)['status'],'inconclusive')

    def test_malformed_cli_is_structured_inconclusive(self):
        for kind in ('offset','run'):
            args=receipts()
            if kind=='offset':args[0]['result']['control']['clock_offset_bounds_ns']=[[]]
            else:args[0]['result']['run']=[]
            with tempfile.TemporaryDirectory() as directory:
                command=[sys.executable,str(Path(gate.__file__))]
                for key,value in zip(('sender','receiver','router','application'),args):
                    path=Path(directory)/key;path.write_text(json.dumps(value));command += ['--'+key,str(path)]
                result=subprocess.run(command,capture_output=True,text=True)
                self.assertEqual(result.returncode,1)
                self.assertEqual(json.loads(result.stdout)['status'],'inconclusive')
                self.assertEqual(result.stderr,'')


if __name__=='__main__':unittest.main()
