import tempfile
import unittest
from pathlib import Path
from unittest import mock

from matcher import compute_detection_status
from scanner import scan_file


class ScannerTest(unittest.TestCase):
    def test_compute_detection_status_thresholds(self):
        self.assertEqual(compute_detection_status(100), (True, "sensitive"))
        self.assertEqual(compute_detection_status(80), (True, "sensitive"))
        self.assertEqual(compute_detection_status(79), (False, "suspected"))
        self.assertEqual(compute_detection_status(50), (False, "suspected"))
        self.assertEqual(compute_detection_status(49), (False, "low_confidence"))
        self.assertEqual(compute_detection_status(30), (False, "low_confidence"))
        self.assertEqual(compute_detection_status(29), (False, "clean"))

    def test_scan_file_records_semantic_label_hits(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "customer.txt"
            path.write_text("客户联系人名单包含电话和报价金额", encoding="utf-8")

            result = scan_file(
                path,
                rules=[],
                fingerprints=[],
                semantic_labels={
                    "file_1": {
                        "semantic_labels": ["客户名单"],
                        "embedding_id": "emb_1",
                    }
                },
            )

        self.assertEqual(result.match_score, 20)
        self.assertEqual(result.sensitive_type, "客户名单")
        self.assertEqual(result.sensitive_file_id, "file_1")
        self.assertEqual(result.match_detail["semantic_label_hits"][0]["semantic_label"], "客户名单")

    def test_scan_file_records_extract_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "broken.pdf"
            path.write_bytes(b"not a valid pdf")

            with mock.patch("scanner.extract_text", side_effect=RuntimeError("parser failed")):
                result = scan_file(path, rules=[], fingerprints=[], semantic_labels={})

        self.assertEqual(result.match_score, 0)
        self.assertEqual(result.match_detail["extract_error"], "parser failed")
        self.assertEqual(result.match_detail["skip_reason"], "pdf_text_empty")


if __name__ == "__main__":
    unittest.main()
