"""客户端目录扫描模块"""

import json
import os
from pathlib import Path
from typing import Iterable, Optional

import chardet
from loguru import logger

try:
    from docx import Document
except Exception:
    Document = None

try:
    import openpyxl
except Exception:
    openpyxl = None

try:
    from pypdf import PdfReader
except Exception:
    PdfReader = None

try:
    from local_db import LocalDB
    from matcher import (
        compute_detection_status,
        compute_score,
        compute_sha256,
        compute_simhash,
        match_combined,
        match_keyword,
        match_regex,
        match_semantic_labels,
        match_sha256,
        match_simhash,
        score_to_risk,
    )
except ImportError:
    from client.local_db import LocalDB
    from client.matcher import (
        compute_detection_status,
        compute_score,
        compute_sha256,
        compute_simhash,
        match_combined,
        match_keyword,
        match_regex,
        match_semantic_labels,
        match_sha256,
        match_simhash,
        score_to_risk,
    )

TEXT_SUFFIXES = {
    ".txt", ".csv", ".json", ".xml", ".md", ".py", ".java", ".go", ".sql",
    ".conf", ".config", ".ini", ".yaml", ".yml", ".log", ".properties",
}
OFFICE_SUFFIXES = {".docx", ".xlsx", ".pdf"}
SKIP_DIRS = {".git", ".hg", ".svn", ".venv", "venv", "node_modules", "__pycache__", "dist", "build", "target", "out", ".mypy_cache", ".pytest_cache", ".idea", ".vscode"}
DEFAULT_MAX_FILE_SIZE = 50 * 1024 * 1024
MAX_FILE_SIZE = int(os.getenv("SCANNER_MAX_FILE_SIZE", DEFAULT_MAX_FILE_SIZE))


class ScanResult:
    def __init__(self, file_path: str, file_hash: str, sensitive: bool, sensitive_type: Optional[str], risk_level: str, sensitive_file_id: Optional[str], match_score: int, confidence_level: str, match_detail: dict):
        self.file_path = file_path
        self.file_hash = file_hash
        self.sensitive = sensitive
        self.sensitive_type = sensitive_type
        self.risk_level = risk_level
        self.sensitive_file_id = sensitive_file_id
        self.match_score = match_score
        self.confidence_level = confidence_level
        self.match_detail = match_detail

    def to_dict(self) -> dict:
        return {
            "file_path": self.file_path,
            "file_hash": self.file_hash,
            "sensitive": self.sensitive,
            "sensitive_type": self.sensitive_type,
            "risk_level": self.risk_level,
            "sensitive_file_id": self.sensitive_file_id,
            "match_score": self.match_score,
            "confidence_level": self.confidence_level,
            "match_detail": self.match_detail,
        }


def scan_directory(path: str, db: LocalDB) -> list[dict]:
    root = Path(path)
    if not root.exists():
        raise FileNotFoundError(f"扫描路径不存在: {path}")

    rules = db.load_rules()
    fingerprints = db.load_fingerprints()
    semantic_labels = db.load_semantic_labels()
    logger.info(f"加载本地规则: {len(rules)} 条, 指纹: {len(fingerprints)} 条, 语义标签: {len(semantic_labels)} 条")

    results = []
    for file_path in iter_files(root):
        try:
            result = scan_file(file_path, rules, fingerprints, semantic_labels)
            db.upsert_file_tag(
                file_path=result.file_path,
                file_hash=result.file_hash,
                sensitive=result.sensitive,
                sensitive_type=result.sensitive_type,
                risk_level=result.risk_level,
                sensitive_file_id=result.sensitive_file_id,
                match_score=result.match_score,
                confidence_level=result.confidence_level,
                match_detail=result.match_detail,
            )
            results.append(result.to_dict())
            if result.confidence_level != "clean":
                logger.warning(f"发现命中文件: {result.file_path}, confidence={result.confidence_level}, score={result.match_score}, risk={result.risk_level}")
            else:
                logger.info(f"扫描完成: {result.file_path}, score={result.match_score}")
        except Exception as exc:
            logger.error(f"扫描失败: {file_path}, reason={exc}")
    return results


def iter_files(root: Path) -> Iterable[Path]:
    if root.is_file():
        yield root
        return

    for file_path in root.rglob("*"):
        if any(part in SKIP_DIRS for part in file_path.parts):
            continue
        if not file_path.is_file():
            continue
        if file_path.stat().st_size > MAX_FILE_SIZE:
            logger.warning(f"跳过过大文件: {file_path}")
            continue
        yield file_path


def scan_file(file_path: Path, rules: list, fingerprints: list, semantic_labels: Optional[dict] = None) -> ScanResult:
    data = file_path.read_bytes()
    sha256 = compute_sha256(data)

    sha_hit = match_sha256(sha256, fingerprints)
    semantic_labels = semantic_labels or {}
    if sha_hit:
        label_detail = semantic_labels.get(sha_hit.get("sensitive_file_id"), {})
        detail = {
            "sha256_hit": True,
            "simhash_hit": False,
            "regex_hits": [],
            "keyword_hits": [],
            "combined_hits": [],
            "semantic_label_hits": [],
            "semantic_labels": label_detail.get("semantic_labels", []),
            "embedding_id": label_detail.get("embedding_id"),
        }
        return ScanResult(str(file_path), sha256, True, "样本指纹命中", "high", sha_hit.get("sensitive_file_id"), 100, "sensitive", detail)

    text = ""
    extract_error = None
    skip_reason = None
    try:
        text = extract_text(file_path, data)
    except Exception as exc:
        extract_error = str(exc)
        logger.warning(f"文本提取失败: {file_path}, reason={extract_error}")
    if not text and file_path.suffix.lower() == ".pdf":
        skip_reason = "pdf_text_empty" if PdfReader is not None else "pdf_reader_missing"

    simhash = compute_simhash(text) if text else ""
    sim_hit = match_simhash(simhash, fingerprints) if simhash else None
    regex_hits = match_regex(text, rules) if text else []
    keyword_hits = match_keyword(text, rules) if text else []
    combined_hits = match_combined(text, rules) if text else []
    semantic_hits = match_semantic_labels(text, semantic_labels) if text else []

    score = compute_score(bool(sha_hit), bool(sim_hit), regex_hits, keyword_hits, combined_hits, semantic_hits)
    sensitive, confidence_level = compute_detection_status(score)
    risk_level = score_to_risk(score)
    sensitive_type = infer_sensitive_type(regex_hits, keyword_hits, combined_hits, rules)
    if sensitive_type is None and semantic_hits:
        sensitive_type = semantic_hits[0].get("semantic_label") or "语义标签命中"
    sensitive_file_id = sim_hit.get("sensitive_file_id") if sim_hit else None
    if sensitive_file_id is None and semantic_hits:
        sensitive_file_id = semantic_hits[0].get("sensitive_file_id")
    label_detail = semantic_labels.get(sensitive_file_id, {}) if sensitive_file_id else {}

    detail = {
        "sha256_hit": False,
        "simhash_hit": bool(sim_hit),
        "simhash": simhash,
        "regex_hits": regex_hits,
        "keyword_hits": keyword_hits,
        "combined_hits": combined_hits,
        "semantic_label_hits": semantic_hits,
        "semantic_labels": label_detail.get("semantic_labels", []),
        "embedding_id": label_detail.get("embedding_id"),
    }
    if extract_error:
        detail["extract_error"] = extract_error
    if skip_reason:
        detail["skip_reason"] = skip_reason
    return ScanResult(str(file_path), sha256, sensitive, sensitive_type, risk_level, sensitive_file_id, score, confidence_level, detail)


def extract_text(file_path: Path, data: bytes) -> str:
    suffix = file_path.suffix.lower()
    if suffix in TEXT_SUFFIXES:
        return read_text_with_detection(data)
    if suffix == ".docx":
        return extract_docx(file_path)
    if suffix == ".xlsx":
        return extract_xlsx(file_path)
    if suffix == ".pdf":
        return extract_pdf(file_path)
    return read_text_with_detection(data)


def read_text_with_detection(data: bytes) -> str:
    detection = chardet.detect(data)
    encoding = detection.get("encoding") or "utf-8"
    try:
        return data.decode(encoding, errors="ignore")
    except LookupError:
        return data.decode("utf-8", errors="ignore")


def extract_docx(file_path: Path) -> str:
    if Document is None:
        logger.warning("未安装 python-docx，跳过 docx 文本提取")
        return ""
    doc = Document(str(file_path))
    parts = [p.text for p in doc.paragraphs if p.text]
    for table in doc.tables:
        for row in table.rows:
            parts.extend(cell.text for cell in row.cells if cell.text)
    return "\n".join(parts)


def extract_xlsx(file_path: Path) -> str:
    if openpyxl is None:
        logger.warning("未安装 openpyxl，跳过 xlsx 文本提取")
        return ""
    wb = openpyxl.load_workbook(str(file_path), read_only=True, data_only=True)
    parts = []
    for sheet in wb.worksheets:
        for row in sheet.iter_rows(values_only=True):
            values = [str(cell) for cell in row if cell is not None]
            if values:
                parts.append(" ".join(values))
    wb.close()
    return "\n".join(parts)


def extract_pdf(file_path: Path) -> str:
    if PdfReader is None:
        logger.warning("未安装 pypdf，跳过 pdf 文本提取")
        return ""
    reader = PdfReader(str(file_path))
    parts = []
    for page in reader.pages:
        text = page.extract_text() or ""
        if text:
            parts.append(text)
    return "\n".join(parts)


def infer_sensitive_type(regex_hits: list, keyword_hits: list, combined_hits: list, rules: list) -> Optional[str]:
    for hit_group in (keyword_hits, combined_hits, regex_hits):
        for hit in hit_group:
            rule_id = hit.get("rule_id")
            for rule in rules:
                if rule.get("rule_id") == rule_id:
                    return rule.get("sensitive_type")
    return None


def dump_results(results: list[dict]):
    print(json.dumps(results, ensure_ascii=False, indent=2))
