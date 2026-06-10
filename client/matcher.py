"""客户端规则匹配模块"""

import re
import hashlib
from typing import Optional


def compute_sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def compute_simhash(text: str) -> str:
    """SimHash：与服务端 (FNV-1a + 2-char Chinese tokenization) 保持一致"""
    tokens = _tokenize_for_simhash(text)
    vector = [0] * 64
    for token in tokens:
        h = _fnv1a_64(token.lower().encode("utf-8"))
        for i in range(64):
            if h & (1 << i):
                vector[i] += 1
            else:
                vector[i] -= 1
    result = 0
    for i in range(64):
        if vector[i] > 0:
            result |= 1 << i
    return f"{result:016x}"

def _fnv1a_64(data: bytes) -> int:
    """FNV-1a 64-bit hash, 与 Go 的 hash/fnv.New64a() 一致"""
    h = 14695981039346656037
    for b in data:
        h ^= b
        h = (h * 1099511628211) % (1 << 64)
    return h


def hamming_distance(a: str, b: str) -> int:
    try:
        av = int(a, 16)
        bv = int(b, 16)
    except ValueError:
        return 999
    x = av ^ bv
    count = 0
    while x:
        count += 1
        x &= x - 1
    return count


def match_sha256(sha256: str, fingerprints: list) -> Optional[dict]:
    """精确 hash 匹配"""
    for fp in fingerprints:
        if fp.get("sha256") == sha256:
            return fp
    return None


def match_simhash(simhash: str, fingerprints: list, threshold: int = 3) -> Optional[dict]:
    """SimHash 相似匹配"""
    best = None
    best_dist = threshold + 1
    for fp in fingerprints:
        sh = fp.get("simhash", "")
        if not sh:
            continue
        dist = hamming_distance(simhash, sh)
        if dist <= threshold and dist < best_dist:
            best = fp
            best_dist = dist
    return best


def match_regex(text: str, rules: list) -> list:
    """正则匹配，返回命中的规则列表"""
    hits = []
    for rule in rules:
        if rule.get("rule_type") != "regex":
            continue
        content = rule.get("content", {})
        pattern = content.get("pattern", "")
        if not pattern:
            continue
        try:
            matches = re.findall(pattern, text)
            if matches:
                hits.append({
                    "rule_id": rule.get("rule_id"),
                    "rule_name": content.get("name", ""),
                    "pattern": pattern,
                    "risk_level": rule.get("risk_level", "medium"),
                    "match_count": len(matches),
                })
        except re.error:
            continue
    return hits


def match_keyword(text: str, rules: list) -> list:
    """关键词匹配"""
    hits = []
    for rule in rules:
        if rule.get("rule_type") != "keyword":
            continue
        content = rule.get("content", {})
        keywords = content.get("keywords", [])
        min_hits = content.get("min_hits", 1)
        if not keywords:
            continue
        matched = [kw for kw in keywords if kw in text]
        if len(matched) >= min_hits:
            hits.append({
                "rule_id": rule.get("rule_id"),
                "rule_name": content.get("name", rule.get("rule_id")),
                "keywords_matched": matched,
                "risk_level": rule.get("risk_level", "medium"),
            })
    return hits


def match_combined(text: str, rules: list) -> list:
    """组合规则匹配"""
    hits = []
    for rule in rules:
        if rule.get("rule_type") != "combined":
            continue
        content = rule.get("content", {})
        logic = content.get("logic", "AND")
        conditions = content.get("conditions", [])
        if not conditions:
            continue

        cond_results = []
        for cond in conditions:
            cond_type = cond.get("type", "")
            if cond_type == "keyword":
                kw_values = cond.get("value", [])
                min_h = cond.get("min_hits", 1)
                matched_kw = [kw for kw in kw_values if kw in text]
                cond_results.append(len(matched_kw) >= min_h)
            elif cond_type == "regex":
                pattern = cond.get("value", "")
                try:
                    cond_results.append(bool(re.search(pattern, text)))
                except re.error:
                    cond_results.append(False)
            else:
                cond_results.append(False)

        if logic == "AND" and all(cond_results):
            hits.append({
                "rule_id": rule.get("rule_id"),
                "risk_level": rule.get("risk_level", "high"),
                "logic": logic,
                "conditions_met": len(cond_results),
            })
        elif logic == "OR" and any(cond_results):
            hits.append({
                "rule_id": rule.get("rule_id"),
                "risk_level": rule.get("risk_level", "high"),
                "logic": logic,
                "conditions_met": sum(cond_results),
            })
    return hits


def compute_score(
    sha256_hit: bool,
    simhash_hit: bool,
    regex_hits: list,
    keyword_hits: list,
    combined_hits: list,
) -> int:
    """根据命中项计算总分"""
    score = 0
    if sha256_hit:
        score += 100
    if simhash_hit:
        score += 70
    for r in regex_hits:
        level = r.get("risk_level", "medium")
        if level == "high" or level == "critical":
            score += 30
        else:
            score += 15
    for k in keyword_hits:
        score += 30
    for c in combined_hits:
        score += 50
    return min(score, 100)


def score_to_risk(score: int) -> str:
    """分数转风险等级"""
    if score >= 80:
        return "high"
    elif score >= 50:
        return "medium"
    elif score >= 30:
        return "low"
    return "info"


def _tokenize_for_simhash(text: str) -> list:
    """中文 2 字符 bigram + 英文整词分词，与服务端 tokenizeForHash 一致"""
    words = []
    current = []
    def flush():
        if current:
            words.append("".join(current))
            current.clear()
    for r in text:
        is_cjk = "\u4e00" <= r <= "\u9fff"
        if r.isalpha() or r.isdigit() or is_cjk:
            current.append(r)
            if is_cjk and len(current) >= 2:
                flush()
            continue
        flush()
    if current:
        words.append("".join(current))
    return words
