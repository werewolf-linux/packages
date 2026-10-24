"""Exercise the API import and the SQLite binding used by SQLite flows."""
import apsw
from langflow.main import setup_app

assert callable(setup_app)
with apsw.Connection(":memory:") as connection:
    connection.execute("CREATE VIRTUAL TABLE documents USING fts5(body)")
    connection.execute("INSERT INTO documents VALUES ('werewolf sqlite check')")
    assert list(connection.execute(
        "SELECT body FROM documents WHERE documents MATCH 'sqlite'"
    )) == [("werewolf sqlite check",)]
print("langflow: API import and SQLite FTS passed")
