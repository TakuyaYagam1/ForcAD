import hashlib
import secrets


def initialize(db):
    db.executescript("""
    CREATE TABLE IF NOT EXISTS users(team TEXT, name TEXT, password TEXT, PRIMARY KEY(team,name));
    CREATE TABLE IF NOT EXISTS sessions(team TEXT, token TEXT PRIMARY KEY, name TEXT);
    CREATE TABLE IF NOT EXISTS records(team TEXT, owner TEXT, id TEXT PRIMARY KEY, body TEXT);
    """)


def handle(db, team, method, path, body, headers):
    if method == "POST" and path == "register":
        # Fixture authentication exercises extra requests without expensive hashing.
        password = hashlib.sha256(body["password"].encode()).hexdigest()
        db.execute("INSERT INTO users VALUES(?,?,?)", (team, body["name"], password))
        return 201, {"name": body["name"]}
    if method == "POST" and path == "login":
        password = hashlib.sha256(body["password"].encode()).hexdigest()
        user = db.execute("SELECT name FROM users WHERE team=? AND name=? AND password=?",
                          (team, body["name"], password)).fetchone()
        if not user:
            return 403, {"error": "invalid credentials"}
        token = secrets.token_hex(16)
        db.execute("INSERT INTO sessions VALUES(?,?,?)", (team, token, user["name"]))
        return 200, {"token": token}
    user = db.execute("SELECT name FROM sessions WHERE team=? AND token=?",
                      (team, headers.get("Authorization", ""))).fetchone()
    if not user:
        return 403, {"error": "authentication required"}
    if method == "POST" and path == "records":
        key = secrets.token_hex(16)
        db.execute("INSERT INTO records VALUES(?,?,?,?)", (team, user["name"], key, body["body"]))
        return 201, {"id": key}
    row = db.execute("SELECT body FROM records WHERE team=? AND owner=? AND id=?",
                     (team, user["name"], path.removeprefix("records/"))).fetchone()
    return (200, dict(row)) if row else (404, {"error": "not found"})
