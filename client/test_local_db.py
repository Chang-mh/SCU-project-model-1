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
                    confidence_level="sensitive",
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
                    confidence_level="low_confidence",
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
        self.assertEqual(rows[0]["confidence_level"], "low_confidence")

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
    def test_save_and_load_semantic_labels(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "tags.db"
            db = LocalDB(str(db_path))
            try:
                db.save_rules(
                    [],
                    [],
                    [{
                        "sensitive_file_id": "file_1",
                        "semantic_labels": ["客户名单", "报价信息"],
                        "embedding_id": "emb_1",
                        "model_name": "rule-fallback",
                    }],
                )
                labels = db.load_semantic_labels()
            finally:
                db.close()

        self.assertEqual(labels["file_1"]["semantic_labels"], ["客户名单", "报价信息"])
        self.assertEqual(labels["file_1"]["embedding_id"], "emb_1")
        self.assertEqual(labels["file_1"]["model_name"], "rule-fallback")
    def test_save_and_load_config(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "tags.db"
            db = LocalDB(str(db_path))
            try:
                db.save_config({"simhash_threshold": 5, "semantic_label_hints": {"客户名单": ["客户名称"]}})
                config = db.load_config()
            finally:
                db.close()

        self.assertEqual(config["simhash_threshold"], 5)
        self.assertEqual(config["semantic_label_hints"], {"客户名单": ["客户名称"]})

    def test_delete_rules_removes_cached_rules(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "tags.db"
            db = LocalDB(str(db_path))
            try:
                db.save_rules([
                    {"rule_id": "rule_1", "rule_type": "regex", "sensitive_type": "固定格式", "risk_level": "high", "content": {"pattern": "secret"}},
                    {"rule_id": "rule_2", "rule_type": "keyword", "sensitive_type": "客户资料", "risk_level": "medium", "content": {"keywords": ["客户"]}},
                ], [], [])
                db.delete_rules(["rule_1"])
                rules = db.load_rules()
            finally:
                db.close()

        self.assertEqual([rule["rule_id"] for rule in rules], ["rule_2"])

    def test_clear_rule_cache_removes_synced_rule_data(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "tags.db"
            db = LocalDB(str(db_path))
            try:
                db.save_config({"simhash_threshold": 5})
                db.save_rules(
                    [{"rule_id": "rule_1", "rule_type": "regex", "sensitive_type": "固定格式", "risk_level": "high", "content": {"pattern": "secret"}}],
                    [{"sensitive_file_id": "file_1", "sha256": "abc", "simhash": "def"}],
                    [{"sensitive_file_id": "file_1", "semantic_labels": ["客户名单"], "embedding_id": "emb_1", "model_name": "rule-fallback"}],
                )
                db.clear_rule_cache()
                rules = db.load_rules()
                fingerprints = db.load_fingerprints()
                labels = db.load_semantic_labels()
                config = db.load_config()
            finally:
                db.close()

        self.assertEqual(rules, [])
        self.assertEqual(fingerprints, [])
        self.assertEqual(labels, {})
        self.assertEqual(config, {})


if __name__ == "__main__":
    unittest.main()
