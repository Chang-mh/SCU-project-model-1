import unittest

from matcher import (
    compute_score,
    compute_simhash,
    match_keyword,
    match_regex,
    match_simhash,
)


class MatcherTest(unittest.TestCase):
    def test_match_regex_detects_common_sensitive_values(self):
        rules = [
            {
                "rule_id": "phone",
                "rule_type": "regex",
                "risk_level": "medium",
                "content": {"name": "mobile_phone", "pattern": r"\b1[3-9]\d{9}\b"},
            },
            {
                "rule_id": "email",
                "rule_type": "regex",
                "risk_level": "medium",
                "content": {"name": "email", "pattern": r"[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}"},
            },
            {
                "rule_id": "id_card",
                "rule_type": "regex",
                "risk_level": "high",
                "content": {"name": "id_card", "pattern": r"\b\d{17}[\dXx]\b"},
            },
        ]

        hits = match_regex("手机号 13800138000，邮箱 test@example.com，身份证 11010119900307123X", rules)

        self.assertEqual({hit["rule_name"] for hit in hits}, {"mobile_phone", "email", "id_card"})

    def test_match_keyword_respects_min_hits(self):
        rules = [
            {
                "rule_id": "customer_keyword",
                "rule_type": "keyword",
                "risk_level": "high",
                "content": {"keywords": ["客户名称", "报价", "合同金额"], "min_hits": 2},
            }
        ]

        self.assertEqual(match_keyword("客户名称：四川示例科技有限公司", rules), [])

        hits = match_keyword("客户名称：四川示例科技有限公司，报价：50万元", rules)
        self.assertEqual(len(hits), 1)
        self.assertEqual(hits[0]["keywords_matched"], ["客户名称", "报价"])

    def test_compute_score_sums_hits_and_caps_at_100(self):
        regex_hits = [{"risk_level": "high"}, {"risk_level": "medium"}]
        keyword_hits = [{"rule_id": "keyword"}]
        combined_hits = [{"rule_id": "combined"}]

        self.assertEqual(compute_score(False, False, regex_hits, keyword_hits, []), 75)
        self.assertEqual(compute_score(False, True, [], [], []), 70)
        self.assertEqual(compute_score(True, True, regex_hits, keyword_hits, combined_hits), 100)

    def test_match_simhash_finds_near_duplicate(self):
        simhash = compute_simhash("客户名称：四川示例科技有限公司\n联系人：张三\n报价：50万元")
        fingerprints = [
            {"sensitive_file_id": "far", "simhash": "0000000000000000"},
            {"sensitive_file_id": "near", "simhash": simhash},
        ]

        hit = match_simhash(simhash, fingerprints, threshold=3)

        self.assertIsNotNone(hit)
        self.assertEqual(hit["sensitive_file_id"], "near")


if __name__ == "__main__":
    unittest.main()
