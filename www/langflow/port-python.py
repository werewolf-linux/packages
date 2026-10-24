"""Port pinned dependencies to Python 3.15."""
import re
import sys
from pathlib import Path

site = Path(sys.argv[1])
for package in ("langchain", "langchain_classic"):
    path = site / package / "chains/base.py"
    if not path.exists():
        continue
    source = path.read_text()
    # Chain.dict shadows the builtin in the class namespace. Python 3.15
    # defers annotations, so Pydantic now evaluates metadata's dict[T, U]
    # with that method in scope. Qualifying the builtin preserves the type.
    if "    def dict(" not in source:
        continue
    source, count = re.subn(r"(?<![\w.])dict\[", "builtins.dict[", source)
    if count and "import builtins" not in source:
        raise RuntimeError(f"{path}: expected upstream builtins import")
    path.write_text(source)
    print(f"{package}: qualified {count} dictionary annotations")

# Python 3.15 removed the deprecated sre_* aliases. LiteLLM's pinned
# complexity router still imports them while Langflow loads components.
path = site / "litellm/router_strategy/complexity_router/config.py"
source = path.read_text()
for old, new in (
    ("import sre_constants", "from re import _constants as sre_constants"),
    ("import sre_parse", "from re import _parser as sre_parse"),
):
    if source.count(old) != 1:
        raise RuntimeError(f"{path}: expected pinned LiteLLM import {old}")
    source = source.replace(old, new, 1)
path.write_text(source)
