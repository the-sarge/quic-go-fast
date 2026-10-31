import copy
import unittest
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


if __name__=='__main__':unittest.main()
