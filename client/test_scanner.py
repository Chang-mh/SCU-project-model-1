import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

from matcher import compute_detection_status
from scanner import is_safe_zip_member, scan_directory, scan_file


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

            with mock.patch("scanner.extract_pdf", side_effect=RuntimeError("parser failed")):
                result = scan_file(path, rules=[], fingerprints=[], semantic_labels={})

        self.assertEqual(result.match_score, 0)
        self.assertEqual(result.match_detail["extract_error"], "parser failed")
        self.assertEqual(result.match_detail["skip_reason"], "pdf_text_empty")

    def test_scan_directory_recurses_into_zip(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            zip_path = tmp_path / "archive.zip"
            db_path = tmp_path / "tags.db"
            with zipfile.ZipFile(zip_path, "w") as archive:
                archive.writestr("docs/customer.txt", "客户名称：四川示例科技有限公司，报价：50万元")

            from local_db import LocalDB
            db = LocalDB(str(db_path))
            try:
                db.save_rules(
                    [{
                        "rule_id": "customer_keyword",
                        "rule_type": "keyword",
                        "sensitive_type": "客户资料",
                        "risk_level": "high",
                        "content": {"keywords": ["客户名称", "报价"], "min_hits": 2},
                    }],
                    [],
                    [],
                )
                results = scan_directory(str(zip_path), db)
            finally:
                db.close()

        self.assertEqual(len(results), 1)
        self.assertIn("archive.zip!docs/customer.txt", results[0]["file_path"])
        self.assertEqual(results[0]["confidence_level"], "low_confidence")
        self.assertEqual(results[0]["sensitive_type"], "客户资料")

    def test_scan_file_marks_unsupported_format(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "slides.pptx"
            path.write_bytes(b"pptx placeholder")

            result = scan_file(path, rules=[], fingerprints=[], semantic_labels={})

        self.assertEqual(result.match_score, 0)
        self.assertEqual(result.match_detail["skip_reason"], "unsupported_format")

    def test_zip_slip_member_is_rejected(self):
        self.assertFalse(is_safe_zip_member("../secret.txt"))
        self.assertFalse(is_safe_zip_member("safe/../../secret.txt"))
        self.assertTrue(is_safe_zip_member("safe/customer.txt"))


if __name__ == "__main__":
    unittest.main()
