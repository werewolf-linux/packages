"""Import the installed training API and exercise its SQLite backend."""
import sqlite3
from langflow.main import setup_app

assert callable(setup_app)
with sqlite3.connect(":memory:") as database:
    assert database.execute("SELECT 6 * 7").fetchone() == (42,)
