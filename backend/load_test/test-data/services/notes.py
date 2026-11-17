import secrets


def initialize(db):
    db.execute(
        "CREATE TABLE IF NOT EXISTS notes(team TEXT, id TEXT, body TEXT, PRIMARY KEY(team,id))"
    )


def handle(db, team, method, path, body, _headers):
    if method == "POST" and path == "notes":
        key = secrets.token_hex(16)
        db.execute("INSERT INTO notes VALUES(?,?,?)", (team, key, body["body"]))
        return 201, {"id": key}
    if method == "GET" and path.startswith("notes/"):
        row = db.execute(
            "SELECT body FROM notes WHERE team=? AND id=?", (team, path[6:])
        ).fetchone()
        return (200, dict(row)) if row else (404, {"error": "not found"})
    return 404, {"error": "not found"}
