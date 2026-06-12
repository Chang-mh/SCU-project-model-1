"""客户端规则同步模块"""

import os

import requests
from loguru import logger

try:
    from local_db import LocalDB
except ImportError:
    from client.local_db import LocalDB


def auth_headers(token: str | None = None) -> dict:
    token = (token or os.getenv("SERVER_API_TOKEN") or "").strip()
    if not token or token == "change-me":
        return {}
    return {"Authorization": f"Bearer {token}"}


def sync_rules(server_url: str, db: LocalDB, token: str | None = None) -> dict:
    """从服务端同步规则到本地 SQLite"""
    local_version = db.get_local_version()
    url = f"{server_url}/api/client/rules?version={local_version}"
    logger.info(f"同步规则: 本地版本={local_version}, 请求={url}")

    try:
        resp = requests.get(url, headers=auth_headers(token), timeout=30)
        resp.raise_for_status()
        data = resp.json()
    except requests.RequestException as e:
        logger.error(f"规则同步失败: {e}")
        return {"success": False, "error": str(e)}

    latest_version = data.get("latest_version", 0)
    full_sync = bool(data.get("full_sync", False))
    rules = data.get("rules", [])
    deleted_rule_ids = data.get("deleted_rule_ids", [])
    fingerprints = data.get("fingerprints", [])
    semantic_labels = data.get("semantic_labels", [])
    config = data.get("config", {})
    if full_sync:
        db.clear_rule_cache()
    if config:
        db.save_config(config)

    if not rules and not fingerprints and not semantic_labels and not deleted_rule_ids and latest_version <= local_version:
        logger.info("规则已是最新，无需更新")
        return {"success": True, "updated": False, "version": local_version}

    db.delete_rules(deleted_rule_ids)
    db.save_rules(rules, fingerprints, semantic_labels)
    db.update_local_version(latest_version)
    logger.info(f"规则同步成功: 新增{len(rules)}条规则, {len(fingerprints)}条指纹, {len(semantic_labels)}条语义标签, 版本={latest_version}")
    return {
        "success": True,
        "updated": True,
        "version": latest_version,
        "rules_count": len(rules),
        "deleted_rules_count": len(deleted_rule_ids),
        "fingerprints_count": len(fingerprints),
        "semantic_labels_count": len(semantic_labels),
        "config": config,
    }
