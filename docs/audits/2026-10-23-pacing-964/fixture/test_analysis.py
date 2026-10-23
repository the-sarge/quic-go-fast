import unittest
from analyze import quantiles, retransmission, ratio
from campaign import schedule

class AnalysisTest(unittest.TestCase):
    def test_quantiles_and_zero_baseline(self):
        self.assertEqual(quantiles(list(range(1,21))),dict(n=20,p50=10,p95=19,max=20,insufficient=False))
        self.assertTrue(quantiles([1]*19)['insufficient'])
        self.assertEqual(ratio(0,0),1)
        self.assertIsNone(ratio(1,0))
    def test_retransmission_uses_stream_intervals(self):
        events=[{'Name':'transport:packet_sent','Data':{'Streams':[{'StreamID':0,'Offset':a,'Length':n}]}} for a,n in [(0,100),(50,100),(0,150)]]
        self.assertEqual(retransmission(events),dict(stream_bytes_sent=350,repeated_stream_bytes=200,repeated_fraction=200/350))
    def test_finite_schedule(self):
        rows=schedule()
        self.assertEqual(len(rows),292)
        self.assertEqual(rows,schedule())
        self.assertEqual(sum(r['stage']=='calibration' for r in rows),48)
        self.assertEqual(sum(r['stage']=='comparison' for r in rows),240)
        self.assertEqual(len(set(r['id'] for r in rows)),292)
        self.assertEqual([r['arm'] for r in rows if r['pair']=='1-01'],['control','candidate'])
        self.assertEqual([r['arm'] for r in rows if r['pair']=='2-01'],['candidate','control'])
if __name__=='__main__':unittest.main()
