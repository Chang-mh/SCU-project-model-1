"""客户端规则匹配模块"""

import re
import hashlib
from typing import Optional

try:
    import jieba
except Exception:
    jieba = None


SEMANTIC_LABEL_KEYWORDS = {
    "客户名单": ["客户", "联系人", "客户名称", "名单", "电话", "邮箱", "手机"],
    "报价信息": ["报价", "报价单", "合同金额", "万元", "价格", "单价", "总价"],
    "财务预算": ["财务", "预算", "成本", "利润", "财报", "营收", "报表"],
    "薪资明细": ["薪资", "工资", "奖金", "绩效", "社保", "个税"],
    "保密协议": ["保密", "协议", "不得披露", "商业机密"],
    "研发设计文档": ["研发", "设计", "架构", "技术方案", "系统架构"],
    "源代码说明": ["源代码", "源码", "接口", "函数", "数据库连接", "api_key"],
    "内部培训资料": ["内部培训", "培训资料", "课件", "培训"],
    "未公开财报": ["未公开财报", "财报", "未公开", "利润", "营收"],
    "运维账号": ["账号", "密码", "token", "secret", "运维", "内网", "ssh"],
    "安全漏洞信息": ["漏洞", "CVE", "修复", "攻击"],
    "战略规划": ["战略", "规划", "商业计划", "未公开"],
}


def luhn_check(number: str) -> bool:
    """Luhn 算法校验银行卡号。"""
    digits = []
    for char in number:
        if not char.isdigit():
            return False
        digits.append(int(char))
    if not 16 <= len(digits) <= 19:
        return False

    total = 0
    for index, digit in enumerate(reversed(digits)):
        if index % 2 == 1:
            digit *= 2
            if digit > 9:
                digit -= 9
        total += digit
    return total % 10 == 0


def _filter_regex_matches(rule_name: str, matches: list) -> list:
    normalized = []
    for match in matches:
        if isinstance(match, tuple):
            normalized.append("".join(str(part) for part in match if part))
        else:
            normalized.append(str(match))
    if rule_name != "bank_card":
        return normalized
    return [match for match in normalized if luhn_check(match)]


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
            matches = _filter_regex_matches(content.get("name", ""), re.findall(pattern, text))
            if matches:
                hits.append({
                    "rule_id": rule.get("rule_id"),
                    "rule_name": content.get("name", ""),
                    "pattern": pattern,
                    "risk_level": rule.get("risk_level", "medium"),
                    "match_count": len(matches),
                    "matches": matches,
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
        matched = _match_keywords(text, keywords)
        if len(matched) >= min_hits:
            hits.append({
                "rule_id": rule.get("rule_id"),
                "rule_name": content.get("name", rule.get("rule_id")),
                "keywords_matched": matched,
                "risk_level": rule.get("risk_level", "medium"),
            })
    return hits


def _match_keywords(text: str, keywords: list) -> list:
    matched = []
    token_set = set(jieba.cut(text)) if jieba is not None else set()
    for kw in keywords:
        if kw in text or kw in token_set:
            matched.append(kw)
    return matched

def match_semantic_labels(text: str, semantic_labels: dict, semantic_label_hints: Optional[dict] = None) -> list:
    """语义标签辅助匹配，优先使用服务端同步的标签关键词映射。"""
    label_hints = semantic_label_hints or SEMANTIC_LABEL_KEYWORDS
    hits = []
    seen = set()
    for sensitive_file_id, detail in (semantic_labels or {}).items():
        labels = detail.get("semantic_labels", []) or []
        for label in labels:
            keywords = set(label_hints.get(label, []))
            keywords.add(label)
            matched = _match_keywords(text, list(keywords))
            if not matched:
                continue
            key = (sensitive_file_id, label)
            if key in seen:
                continue
            seen.add(key)
            hits.append({
                "sensitive_file_id": sensitive_file_id,
                "semantic_label": label,
                "keywords_matched": matched,
                "embedding_id": detail.get("embedding_id"),
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
                matched_kw = _match_keywords(text, kw_values)
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
    semantic_hits: Optional[list] = None,
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
    if semantic_hits:
        score += min(30, len(semantic_hits) * 20)
    return min(score, 100)


def compute_detection_status(score: int) -> tuple[bool, str]:
    """返回 (是否真实敏感, 置信分级)。"""
    if score >= 80:
        return True, "sensitive"
    if score >= 50:
        return False, "suspected"
    if score >= 30:
        return False, "low_confidence"
    return False, "clean"


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
