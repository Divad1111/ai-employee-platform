# -*- coding: utf-8 -*-
"""从 agent transcript 重放对 DEVELOPMENT_TASKS.md 的写入与替换。"""
import json
import re

path = r"C:\Users\DZQ-901\.cursor\projects\f-OpenSource-ai-employee-platform\agent-transcripts\675e21c8-241c-48b0-bf59-06d5241a95f6\675e21c8-241c-48b0-bf59-06d5241a95f6.jsonl"
target = r"f:\OpenSource\ai-employee-platform\docs\DEVELOPMENT_TASKS.md"
content = None
ops = 0
misses = 0

with open(path, "r", encoding="utf-8") as f:
    for line in f:
        if "DEVELOPMENT_TASKS.md" not in line:
            continue
        try:
            o = json.loads(line)
        except Exception:
            continue
        for c in o.get("message", {}).get("content", []) or []:
            if c.get("type") != "tool_use":
                continue
            name = c.get("name")
            inp = c.get("input") or {}
            p = str(inp.get("path", "")).replace("/", "\\")
            if "DEVELOPMENT_TASKS.md" not in p:
                continue
            if name == "Write" and "contents" in inp:
                content = inp["contents"]
                ops += 1
            elif name == "StrReplace" and content is not None:
                old, new = inp.get("old_string"), inp.get("new_string")
                if old is None or new is None:
                    continue
                if old in content:
                    content = content.replace(old, new, 1)
                    ops += 1
                else:
                    old_n = old.replace("\r\n", "\n")
                    cur = content.replace("\r\n", "\n")
                    if old_n in cur:
                        content = cur.replace(old_n, new.replace("\r\n", "\n"), 1)
                        ops += 1
                    else:
                        misses += 1

if content is None:
    raise SystemExit("no content")

# 勾选 M0+M1：T-0101 … T-0121
content = re.sub(
    r"#### (T-01(?:0[1-9]|1[0-9]|2[01])) \[ \]",
    r"#### \1 [x]",
    content,
)

content = content.replace(
    "> **范围**：V1 Golden Path 必须完成；V2/V3 仅登记标题，不展开实现细节。",
    "> **范围**：当前开发 **V1（M0–M6）→ V2（M7–M9）**。**V3（M10–M12）仅作路线图保留，暂不开发。**",
)

# 决策结论（若仍为空）
pairs = [
    (
        "| **Q-02** | Employee 飞书身份：每 Employee 一应用 Bot，还是单 Bot + 指令/@别名路由？              | §11, §82      | M6  | A) 每 Employee 独立飞书应用 B) 单 Bot，消息内解析目标 Employee C) 单应用多机器人能力（若飞书支持）                                            |     |",
        "| **Q-02** | Employee 飞书身份：每 Employee 一应用 Bot，还是单 Bot + 指令/@别名路由？              | §11, §82      | M6  | A) 每 Employee 独立飞书应用 B) 单 Bot，消息内解析目标 Employee C) 单应用多机器人能力（若飞书支持）                                            | **B（见 DECISIONS.md）** |",
    ),
    (
        "| **Q-03** | V1 `max_sessions`：§51 说每 Employee 仅 1 个 Active Session，§124 示例为 2 | §51, §124     | M4  | A) V1 强制 1，配置项保留但忽略 >1 B) V1 允许配置到 2                                                                          |     |",
        "| **Q-03** | V1 `max_sessions`：§51 说每 Employee 仅 1 个 Active Session，§124 示例为 2 | §51, §124     | M4  | A) V1 强制 1，配置项保留但忽略 >1 B) V1 允许配置到 2                                                                          | **A（见 DECISIONS.md）** |",
    ),
    (
        "| **Q-05** | `aew daemon` 是否正式进入 CLI 树？与 `aew service` 关系？                     | §40, §123     | M5  | A) 增加 `daemon` 子命令 B) 仅 `service` 启动，无独立 daemon 命令                                                            |     |",
        "| **Q-05** | `aew daemon` 是否正式进入 CLI 树？与 `aew service` 关系？                     | §40, §123     | M5  | A) 增加 `daemon` 子命令 B) 仅 `service` 启动，无独立 daemon 命令                                                            | **A（见 DECISIONS.md）** |",
    ),
]
for a, b in pairs:
    if a in content:
        content = content.replace(a, b)

with open(target, "w", encoding="utf-8", newline="\n") as out:
    out.write(content)

print("ops", ops, "misses", misses, "len", len(content))
print("T-0901", "T-0901" in content)
print("T-0107 [x]", "T-0107 [x]" in content)
print("T-0121 [x]", "T-0121 [x]" in content)
print("V3 defer", "暂不开发" in content or "暂缓" in content)
print("marks", len(re.findall(r"T-01(?:0[1-9]|1[0-9]|2[01]) \[x\]", content)))
