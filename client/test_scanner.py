import unittest

from matcher import compute_detection_status


class ScannerTest(unittest.TestCase):
    def test_compute_detection_status_thresholds(self):
        self.assertEqual(compute_detection_status(100), (True, "sensitive"))
        self.assertEqual(compute_detection_status(80), (True, "sensitive"))
        self.assertEqual(compute_detection_status(79), (False, "suspected"))
        self.assertEqual(compute_detection_status(50), (False, "suspected"))
        self.assertEqual(compute_detection_status(49), (False, "low_confidence"))
        self.assertEqual(compute_detection_status(30), (False, "low_confidence"))
        self.assertEqual(compute_detection_status(29), (False, "clean"))


if __name__ == "__main__":
    unittest.main()
