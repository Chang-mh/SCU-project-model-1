"""敏感文件识别客户端命令行入口"""

import argparse
import json
import socket
from datetime import datetime
from pathlib import Path

import requests
from loguru import logger

try:
    from local_db import LocalDB
    from scanner import dump_results, scan_directory
    from sync import auth_headers, sync_rules
except ImportError:
    from client.local_db import LocalDB
    from client.scanner import dump_results, scan_directory
    from client.sync import auth_headers, sync_rules


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="敏感文件识别客户端")
    parser.add_argument("--db", default="sensitive_tags.db", help="本地 SQLite 数据库路径")
    subparsers = parser.add_subparsers(dest="command", required=True)

    sync_parser = subparsers.add_parser("sync", help="同步服务端敏感文件规则库")
    sync_parser.add_argument("--server", default="http://127.0.0.1:8080", help="服务端地址")
    sync_parser.add_argument("--token", default=None, help="API Token；未提供时读取 SERVER_API_TOKEN")

    scan_parser = subparsers.add_parser("scan", help="扫描指定目录或文件")
    scan_parser.add_argument("--path", required=True, help="需要扫描的目录或文件")
    scan_parser.add_argument("--server", default="http://127.0.0.1:8080", help="服务端地址；扫描前会先尝试同步")
    scan_parser.add_argument("--token", default=None, help="API Token；未提供时读取 SERVER_API_TOKEN")
    scan_parser.add_argument("--no-sync", action="store_true", help="扫描前不自动同步规则")
    scan_parser.add_argument("--json", action="store_true", help="以 JSON 输出扫描结果")
    scan_parser.add_argument("--report", action="store_true", help="扫描完成后将结果上报服务端")
    scan_parser.add_argument("--host-id", default=socket.gethostname(), help="扫描结果上报使用的主机标识")

    list_parser = subparsers.add_parser("list", help="查看本地扫描标签")
    list_parser.add_argument("--sensitive-only", action="store_true", help="只显示敏感文件")
    list_parser.add_argument("--json", action="store_true", help="以 JSON 输出标签")

    subparsers.add_parser("clear", help="清空本地扫描标签")
    return parser


def report_scan_results(server_url: str, results: list[dict], scan_path: str, host_id: str, token: str | None = None) -> dict:
    payload = {
        "host_id": host_id,
        "scan_path": scan_path,
        "scanned_at": datetime.now().isoformat(timespec="seconds"),
        "results": results,
    }
    url = f"{server_url.rstrip('/')}/api/client/scan-results"
    try:
        resp = requests.post(url, json=payload, headers=auth_headers(token), timeout=30)
        resp.raise_for_status()
        data = resp.json()
        logger.info(f"扫描结果上报成功: {data}")
        return {"success": True, **data}
    except requests.RequestException as exc:
        logger.error(f"扫描结果上报失败: {exc}")
        return {"success": False, "error": str(exc)}


def truncate_text(value: str, max_len: int = 56) -> str:
    value = str(value or "")
    if len(value) <= max_len:
        return value
    return value[: max_len - 3] + "..."


def print_tags_table(rows: list[dict]):
    headers = ["文件路径", "置信度", "分数", "风险", "敏感类型", "最后检测时间"]
    print(" | ".join(headers))
    print("-" * 100)
    for row in rows:
        print(" | ".join([
            truncate_text(row.get("file_path"), 42),
            truncate_text(row.get("confidence_level") or "clean", 14),
            str(row.get("match_score") or 0),
            truncate_text(row.get("risk_level") or "info", 8),
            truncate_text(row.get("sensitive_type") or "", 18),
            truncate_text(row.get("last_detected_at") or "", 19),
        ]))


def main():
    parser = build_parser()
    args = parser.parse_args()
    db = LocalDB(args.db)

    try:
        if args.command == "sync":
            result = sync_rules(args.server.rstrip("/"), db, token=args.token)
            print(json.dumps(result, ensure_ascii=False, indent=2))
        elif args.command == "scan":
            target = Path(args.path)
            if not target.exists():
                raise FileNotFoundError(f"扫描路径不存在: {args.path}")
            if not args.no_sync:
                sync_rules(args.server.rstrip("/"), db, token=args.token)
            results = scan_directory(args.path, db)
            if args.json:
                dump_results(results)
            else:
                total = len(results)
                sensitive = sum(1 for r in results if r.get("confidence_level") == "sensitive")
                suspected = sum(1 for r in results if r.get("confidence_level") == "suspected")
                low_confidence = sum(1 for r in results if r.get("confidence_level") == "low_confidence")
                print(f"扫描完成：总文件 {total}，敏感 {sensitive}，疑似 {suspected}，低置信 {low_confidence}")
                label_map = {"sensitive": "[敏感]", "suspected": "[疑似]", "low_confidence": "[低置信]"}
                for r in results:
                    label = label_map.get(r.get("confidence_level"))
                    if label:
                        print(f"{label} {r['file_path']} score={r['match_score']} risk={r['risk_level']} type={r.get('sensitive_type')}")
            if args.report:
                report = report_scan_results(args.server.rstrip("/"), results, args.path, args.host_id, token=args.token)
                print(json.dumps(report, ensure_ascii=False, indent=2))
        elif args.command == "list":
            rows = db.list_tags(args.sensitive_only)
            if args.json:
                print(json.dumps(rows, ensure_ascii=False, indent=2))
            else:
                print_tags_table(rows)
        elif args.command == "clear":
            db.clear_tags()
            logger.info("已清空本地扫描标签")
    finally:
        db.close()


if __name__ == "__main__":
    main()
