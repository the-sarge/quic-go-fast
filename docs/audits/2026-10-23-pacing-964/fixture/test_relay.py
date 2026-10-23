import unittest
from relay import Direction, selected

class RelayTest(unittest.TestCase):
    def test_serialization_and_capacity_include_in_service(self):
        d=Direction(10,100,'clean',96400,0)
        a=d.admit(0,b'a'*1280)
        b=d.admit(0,b'b'*1280)
        self.assertEqual((a['wait_ns'],a['serial_ns'],a['due_ns']),(0,1024000,51024000))
        self.assertEqual(b['wait_ns'],1024000)
        self.assertEqual(d.capacity,125000)
        while d.admit(0,b'x'*1280)['status']=='admitted': pass
        self.assertLessEqual(d.queued_bytes,d.capacity)
        self.assertEqual(d.admit(0,b'z'*1280)['status'],'overflow')
        self.assertEqual(d.admit(200000000,b'x'*1280)['wait_ns'],0)
    def test_reproducible_direction_local_impairment(self):
        self.assertEqual([selected(96401,0,n,1000) for n in range(5000)], [selected(96401,0,n,1000) for n in range(5000)])
        self.assertNotEqual([selected(96401,0,n,1000) for n in range(5000)], [selected(96401,1,n,1000) for n in range(5000)])
        d=Direction(100,10,'reorder',96401,0)
        rows=[d.admit(n*1000000,b'x'*1280) for n in range(1000)]
        self.assertTrue(any(r['reorder_ns']==10000000 for r in rows))
        self.assertTrue(all(r['status']=='admitted' for r in rows))
    def test_drop_does_not_consume_queue(self):
        d=Direction(100,10,'loss',96401,0)
        rows=[d.admit(n*1000000,b'x'*1280) for n in range(10000)]
        self.assertTrue(any(r['status']=='injected_drop' for r in rows))
        self.assertTrue(all(r['status']!='overflow' for r in rows))

if __name__=='__main__': unittest.main()
