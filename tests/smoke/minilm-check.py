"""Check the installed MiniLM model and tokenizer metadata."""
import json
from pathlib import Path

root = Path("/usr/share/onnx/minilm-l6-v2/onnx")
assert (root / "model.onnx").stat().st_size > 1000000
assert (root / "vocab.txt").stat().st_size > 1000
for name in (
    "config.json", "tokenizer.json", "tokenizer_config.json", "special_tokens_map.json",
):
    json.loads((root / name).read_text())
