"""敏感文件识别客户端命令行入口"""

import argparse
import json
from pathlib import Path

from loguru import logger

try:
    from local_db import LocalDB
    from scanner import dump_results, scan_directory
    from sync import sync_rules
except ImportError:
    from client.local_db import LocalDB
    from client.scanner import dump_results, scan_directory
    from client.sync import sync_rules


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="敏感文件识别客户端")
    parser.add_argument("--db", default="sensitive_tags.db", help="本地 SQLite 数据库路径")
    subparsers = parser.add_subparsers(dest="command", required=True)

    sync_parser = subparsers.add_parser("sync", help="同步服务端敏感文件规则库")
    sync_parser.add_argument("--server", default="http://127.0.0.1:8080", help="服务端地址")

    scan_parser = subparsers.add_parser("scan", help="扫描指定目录或文件")
    scan_parser.add_argument("--path", required=True, help="需要扫描的目录或文件")
    scan_parser.add_argument("--server", default="http://127.0.0.1:8080", help="服务端地址；扫描前会先尝试同步")
    scan_parser.add_argument("--no-sync", action="store_true", help="扫描前不自动同步规则")
    scan_parser.add_argument("--json", action="store_true", help="以 JSON 输出扫描结果")

    list_parser = subparsers.add_parser("list", help="查看本地扫描标签")
    list_parser.add_argument("--sensitive-only", action="store_true", help="只显示敏感文件")

    subparsers.add_parser("clear", help="清空本地扫描标签")
    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()
    db = LocalDB(args.db)

    try:
        if args.command == "sync":
            result = sync_rules(args.server.rstrip("/"), db)
            print(json.dumps(result, ensure_ascii=False, indent=2))
        elif args.command == "scan":
            target = Path(args.path)
            if not target.exists():
                raise FileNotFoundError(f"扫描路径不存在: {args.path}")
            if not args.no_sync:
                sync_rules(args.server.rstrip("/"), db)
            results = scan_directory(args.path, db)
            if args.json:
                dump_results(results)
            else:
                total = len(results)
                sensitive = sum(1 for r in results if r.get("sensitive"))
                print(f"扫描完成：总文件 {total}，敏感/疑似敏感 {sensitive}")
                for r in results:
                    if r.get("sensitive"):
                        print(f"[敏感] {r['file_path']} score={r['match_score']} risk={r['risk_level']} type={r.get('sensitive_type')}")
        elif args.command == "list":
            rows = db.list_tags(args.sensitive_only)
            print(json.dumps(rows, ensure_ascii=False, indent=2))
        elif args.command == "clear":
            db.clear_tags()
            logger.info("已清空本地扫描标签")
    finally:
        db.close()


if __name__ == "__main__":
    main()
