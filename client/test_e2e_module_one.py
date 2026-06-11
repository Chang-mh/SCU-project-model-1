import os
import tempfile
import unittest
from pathlib import Path

import requests

from local_db import LocalDB
from scanner import scan_directory
from sync import auth_headers, sync_rules


@unittest.skipUnless(os.getenv("MODULE_ONE_E2E_SERVER"), "设置 MODULE_ONE_E2E_SERVER 后运行端到端测试")
class ModuleOneE2ETest(unittest.TestCase):
    def test_upload_sync_and_scan_identical_sample(self):
        server = os.getenv("MODULE_ONE_E2E_SERVER", "").rstrip("/")
        sample_text = "客户名称：四川示例科技有限公司\n联系人：张三\n电话：13800138000\n报价：50万元\n"

        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            sample_path = tmp_path / "customer.txt"
            sample_path.write_text(sample_text, encoding="utf-8")

            with sample_path.open("rb") as fh:
                resp = requests.post(
                    f"{server}/api/server/samples",
                    files={"file": (sample_path.name, fh, "text/plain")},
                    data={"sensitive_type": "客户报价", "risk_level": "high"},
                    headers=auth_headers(),
                    timeout=30,
                )
            resp.raise_for_status()

            db = LocalDB(str(tmp_path / "tags.db"))
            try:
                sync_result = sync_rules(server, db)
                self.assertTrue(sync_result["success"])
                self.assertGreaterEqual(sync_result.get("fingerprints_count", 0), 1)

                results = scan_directory(str(sample_path), db)
            finally:
                db.close()

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["confidence_level"], "sensitive")
        self.assertEqual(results[0]["match_score"], 100)


if __name__ == "__main__":
    unittest.main()
