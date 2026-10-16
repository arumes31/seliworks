"""Validate GitHub's self-repository references with actionlint 1.7.12.

GitHub supports $/.github/workflows/... at the caller's commit, but actionlint
does not yet parse it (upstream issue rhysd/actionlint#711). Validate that these
references name real local workflows, then translate only this equivalent
reference spelling in stdin. All actionlint rules still run. The actual files
retain $/ and are independently checked by zizmor. Remove this adapter when the
pinned actionlint release implements upstream support.
"""

from pathlib import Path
import re
import subprocess


for workflow in sorted(Path(".github/workflows").glob("*.y*ml")):
    lines = workflow.read_text(encoding="utf-8").splitlines(keepends=True)
    for index, line in enumerate(lines):
        if re.search(r"^\s*uses:\s+\$/", line):
            match = re.fullmatch(
                r"\s*uses:\s+\$/(\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml)\s*(?:#.*)?",
                line.strip(),
            )
            if not match or not Path(match[1]).is_file():
                raise SystemExit(f"Invalid self-repository workflow: {workflow}:{index + 1}")
            lines[index] = line.replace("$/", "./", 1)
    subprocess.run(
        ["actionlint", "-stdin-filename", str(workflow), "-"],
        input="".join(lines),
        text=True,
        check=True,
    )
