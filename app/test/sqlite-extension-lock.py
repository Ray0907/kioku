#!/usr/bin/env python3
"""CLI E2E: CJK auto-extension initialization must honor the database lock policy."""
import json
import os
import pathlib
import sqlite3
import subprocess
import sys
import tempfile

binary = str(pathlib.Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="kioku-extension-lock-") as home:
    database = pathlib.Path(home) / "index.db"
    connection = sqlite3.connect(database)
    connection.execute("CREATE TABLE lock_probe(n INTEGER)")
    connection.commit()
    connection.execute("BEGIN EXCLUSIVE")
    process = subprocess.Popen([binary, "--json", "--", "紅茶"],
                               env={"HOME": home, "KIOKU_INDEX": str(database), "PATH": os.defpath},
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        try:
            process.wait(timeout=0.5)
        except subprocess.TimeoutExpired:
            pass
        else:
            _, error = process.communicate()
            raise AssertionError("CLI exited while the database was locked: " + error.decode())
        connection.rollback()
        output, error = process.communicate(timeout=10)
        if process.returncode != 0:
            raise AssertionError(error.decode())
        if json.loads(output)["hits"] != []:
            raise AssertionError("Expected an empty isolated index")
    finally:
        connection.rollback()
        connection.close()
        if process.poll() is None:
            process.kill()
        process.communicate()
print("PASS: CJK initialization waits for the database lock, then the real CLI returns JSON")
