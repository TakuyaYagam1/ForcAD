import secrets
import threading
import time


def initialize(db):
    db.execute(
        "CREATE TABLE IF NOT EXISTS jobs(team TEXT, id TEXT PRIMARY KEY, body TEXT, ready INTEGER DEFAULT 0)"
    )
    db.execute("CREATE INDEX IF NOT EXISTS jobs_pending ON jobs(ready) WHERE ready=0")


def start_worker(connect):
    def work():
        while True:
            with connect() as db:
                db.execute("UPDATE jobs SET ready=1 WHERE ready=0")
            time.sleep(0.1)

    threading.Thread(target=work, daemon=True).start()


def handle(db, team, method, path, body, _headers):
    if method == "POST" and path == "jobs":
        key = secrets.token_hex(16)
        db.execute(
            "INSERT INTO jobs(team,id,body) VALUES(?,?,?)", (team, key, body["body"])
        )
        return 202, {"id": key}
    row = db.execute(
        "SELECT body,ready FROM jobs WHERE team=? AND id=?",
        (team, path.removeprefix("jobs/")),
    ).fetchone()
    if not row:
        return 404, {"error": "not found"}
    return (200, {"body": row["body"]}) if row["ready"] else (202, {"pending": True})
