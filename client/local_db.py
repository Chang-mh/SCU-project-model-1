"""客户端本地 SQLite 标签库管理"""

import sqlite3
from datetime import datetime
from typing import Optional


class LocalDB:
    def __init__(self, db_path: str = "sensitive_tags.db"):
        self.db_path = db_path
        self.conn = sqlite3.connect(db_path)
        self.conn.row_factory = sqlite3.Row
        self._init_tables()

    def _init_tables(self):
        cursor = self.conn.cursor()
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS local_file_tags (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                file_path TEXT NOT NULL,
                file_hash TEXT NOT NULL,
                sensitive INTEGER NOT NULL DEFAULT 0,
                sensitive_type TEXT,
                risk_level TEXT,
                sensitive_file_id TEXT,
                match_score INTEGER,
                confidence_level TEXT DEFAULT 'clean',
                match_detail TEXT,
                first_detected_at TEXT,
                last_detected_at TEXT,
                UNIQUE(file_path, file_hash)
            )
        """)
        self._ensure_column("local_file_tags", "confidence_level", "TEXT DEFAULT 'clean'")
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS local_rules_version (
                id INTEGER PRIMARY KEY CHECK (id = 1),
                version INTEGER NOT NULL DEFAULT 0
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS cached_rules (
                rule_id TEXT PRIMARY KEY,
                rule_type TEXT,
                sensitive_type TEXT,
                risk_level TEXT,
                content TEXT
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS cached_fingerprints (
                sensitive_file_id TEXT PRIMARY KEY,
                sha256 TEXT,
                simhash TEXT
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS cached_semantic_labels (
                sensitive_file_id TEXT PRIMARY KEY,
                semantic_labels TEXT,
                embedding_id TEXT,
                model_name TEXT
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS local_config (
                key TEXT PRIMARY KEY,
                value TEXT
            )
        """)
        self.conn.commit()

    def _ensure_column(self, table: str, column: str, definition: str):
        cursor = self.conn.cursor()
        cursor.execute(f"PRAGMA table_info({table})")
        columns = {row["name"] for row in cursor.fetchall()}
        if column not in columns:
            cursor.execute(f"ALTER TABLE {table} ADD COLUMN {column} {definition}")

    def get_local_version(self) -> int:
        cursor = self.conn.cursor()
        cursor.execute("SELECT version FROM local_rules_version WHERE id = 1")
        row = cursor.fetchone()
        return row["version"] if row else 0

    def update_local_version(self, version: int):
        cursor = self.conn.cursor()
        cursor.execute(
            "INSERT OR REPLACE INTO local_rules_version (id, version) VALUES (1, ?)",
            (version,),
        )
        self.conn.commit()

    def save_config(self, config: dict):
        cursor = self.conn.cursor()
        for key, value in (config or {}).items():
            cursor.execute(
                "INSERT OR REPLACE INTO local_config (key, value) VALUES (?, ?)",
                (str(key), str(value)),
            )
        self.conn.commit()

    def load_config(self) -> dict:
        cursor = self.conn.cursor()
        cursor.execute("SELECT key, value FROM local_config")
        config = {}
        for row in cursor.fetchall():
            value = row["value"]
            if value is not None and value.isdigit():
                config[row["key"]] = int(value)
            else:
                config[row["key"]] = value
        return config

    def clear_rule_cache(self):
        cursor = self.conn.cursor()
        cursor.execute("DELETE FROM cached_rules")
        cursor.execute("DELETE FROM cached_fingerprints")
        cursor.execute("DELETE FROM cached_semantic_labels")
        cursor.execute("DELETE FROM local_config")
        self.conn.commit()

    def delete_rules(self, rule_ids):
        if not rule_ids:
            return
        cursor = self.conn.cursor()
        cursor.executemany("DELETE FROM cached_rules WHERE rule_id = ?", [(rule_id,) for rule_id in rule_ids])
        self.conn.commit()

    def save_rules(self, rules, fingerprints, semantic_labels=None):
        cursor = self.conn.cursor()
        for r in rules:
            import json
            cursor.execute(
                "INSERT OR REPLACE INTO cached_rules (rule_id, rule_type, sensitive_type, risk_level, content) VALUES (?, ?, ?, ?, ?)",
                (r.get("rule_id"), r.get("rule_type"), r.get("sensitive_type"), r.get("risk_level"), json.dumps(r.get("content", {}), ensure_ascii=False)),
            )
        for f in fingerprints:
            cursor.execute(
                "INSERT OR REPLACE INTO cached_fingerprints (sensitive_file_id, sha256, simhash) VALUES (?, ?, ?)",
                (f.get("sensitive_file_id"), f.get("sha256"), f.get("simhash")),
            )
        for s in semantic_labels or []:
            import json
            cursor.execute(
                "INSERT OR REPLACE INTO cached_semantic_labels (sensitive_file_id, semantic_labels, embedding_id, model_name) VALUES (?, ?, ?, ?)",
                (s.get("sensitive_file_id"), json.dumps(s.get("semantic_labels", []), ensure_ascii=False), s.get("embedding_id"), s.get("model_name")),
            )
        self.conn.commit()

    def load_rules(self):
        import json
        cursor = self.conn.cursor()
        cursor.execute("SELECT rule_id, rule_type, sensitive_type, risk_level, content FROM cached_rules")
        rules = []
        for row in cursor.fetchall():
            rules.append({
                "rule_id": row["rule_id"],
                "rule_type": row["rule_type"],
                "sensitive_type": row["sensitive_type"],
                "risk_level": row["risk_level"],
                "content": json.loads(row["content"]) if row["content"] else {},
            })
        return rules

    def load_fingerprints(self):
        cursor = self.conn.cursor()
        cursor.execute("SELECT sensitive_file_id, sha256, simhash FROM cached_fingerprints")
        return [
            {"sensitive_file_id": row["sensitive_file_id"], "sha256": row["sha256"], "simhash": row["simhash"]}
            for row in cursor.fetchall()
        ]

    def load_semantic_labels(self):
        import json
        cursor = self.conn.cursor()
        cursor.execute("SELECT sensitive_file_id, semantic_labels, embedding_id, model_name FROM cached_semantic_labels")
        labels = {}
        for row in cursor.fetchall():
            labels[row["sensitive_file_id"]] = {
                "semantic_labels": json.loads(row["semantic_labels"]) if row["semantic_labels"] else [],
                "embedding_id": row["embedding_id"],
                "model_name": row["model_name"],
            }
        return labels

    def upsert_file_tag(
        self,
        file_path: str,
        file_hash: str,
        sensitive: bool,
        sensitive_type: Optional[str] = None,
        risk_level: Optional[str] = None,
        sensitive_file_id: Optional[str] = None,
        match_score: int = 0,
        confidence_level: str = "clean",
        match_detail: Optional[dict] = None,
    ):
        import json
        now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        sensitive_int = 1 if sensitive else 0
        detail_json = json.dumps(match_detail or {}, ensure_ascii=False)

        cursor = self.conn.cursor()
        cursor.execute(
            """INSERT INTO local_file_tags
                (file_path, file_hash, sensitive, sensitive_type, risk_level,
                 sensitive_file_id, match_score, confidence_level, match_detail, first_detected_at, last_detected_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(file_path, file_hash) DO UPDATE SET
                    sensitive = excluded.sensitive,
                    sensitive_type = excluded.sensitive_type,
                    risk_level = excluded.risk_level,
                    sensitive_file_id = excluded.sensitive_file_id,
                    match_score = excluded.match_score,
                    confidence_level = excluded.confidence_level,
                    match_detail = excluded.match_detail,
                    last_detected_at = excluded.last_detected_at
            """,
            (file_path, file_hash, sensitive_int, sensitive_type, risk_level,
             sensitive_file_id, match_score, confidence_level, detail_json, now, now),
        )
        self.conn.commit()

    def list_tags(self, sensitive_only: bool = False):
        cursor = self.conn.cursor()
        if sensitive_only:
            cursor.execute("SELECT * FROM local_file_tags WHERE sensitive = 1 ORDER BY match_score DESC")
        else:
            cursor.execute("SELECT * FROM local_file_tags ORDER BY last_detected_at DESC")
        return [dict(row) for row in cursor.fetchall()]

    def clear_tags(self):
        cursor = self.conn.cursor()
        cursor.execute("DELETE FROM local_file_tags")
        self.conn.commit()

    def close(self):
        self.conn.close()
