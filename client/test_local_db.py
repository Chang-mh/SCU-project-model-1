import tempfile
import unittest
from pathlib import Path

from local_db import LocalDB


class LocalDBTest(unittest.TestCase):
    def test_upsert_file_tag_updates_existing_row(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "tags.db"
            db = LocalDB(str(db_path))
            try:
                db.upsert_file_tag(
                    file_path="/tmp/customer.txt",
                    file_hash="hash-1",
                    sensitive=True,
                    sensitive_type="客户资料",
                    risk_level="high",
                    sensitive_file_id="file_1",
                    match_score=100,
                    match_detail={"sha256_hit": True},
                )
                db.upsert_file_tag(
                    file_path="/tmp/customer.txt",
                    file_hash="hash-1",
                    sensitive=False,
                    sensitive_type="客户资料",
                    risk_level="low",
                    sensitive_file_id="file_1",
                    match_score=30,
                    match_detail={"keyword_hits": ["报价"]},
                )

                rows = db.list_tags()
            finally:
                db.close()

        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["file_path"], "/tmp/customer.txt")
        self.assertEqual(rows[0]["file_hash"], "hash-1")
        self.assertEqual(rows[0]["sensitive"], 0)
        self.assertEqual(rows[0]["risk_level"], "low")
        self.assertEqual(rows[0]["match_score"], 30)

    def test_list_tags_returns_dict_rows_and_filters_sensitive_only(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "tags.db"
            db = LocalDB(str(db_path))
            try:
                db.upsert_file_tag("/tmp/clean.txt", "hash-clean", False, match_score=0)
                db.upsert_file_tag("/tmp/secret.txt", "hash-secret", True, "客户资料", "high", "file_2", 100)

                all_rows = db.list_tags()
                sensitive_rows = db.list_tags(sensitive_only=True)
            finally:
                db.close()

        self.assertEqual(len(all_rows), 2)
        self.assertTrue(all(isinstance(row, dict) for row in all_rows))
        self.assertEqual(len(sensitive_rows), 1)
        self.assertEqual(sensitive_rows[0]["file_path"], "/tmp/secret.txt")


if __name__ == "__main__":
    unittest.main()
