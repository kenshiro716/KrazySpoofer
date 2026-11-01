"""Manage Uploaderv3Newest license keys.

  python tools/license.py add <robloxUserId>
  python tools/license.py revoke <robloxUserId>

Updates licenses.json, which must then be committed and pushed. Keys and user
ids go in licenses.local.json, which must never be committed.
"""
import hashlib
import json
import secrets
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PUBLIC = ROOT / "licenses.json"
LOCAL = ROOT / "licenses.local.json"
ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"


def load(path, default):
    return json.loads(path.read_text()) if path.exists() else default


def save(path, data):
    with open(path, "w", newline="\n") as f:
        f.write(json.dumps(data, indent=2) + "\n")


def new_key():
    groups = ("".join(secrets.choice(ALPHABET) for _ in range(5)) for _ in range(5))
    return "-".join(groups)


def key_hash(key, user_id):
    return hashlib.sha256(f"{key}:{user_id}".encode()).hexdigest()


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ("add", "revoke") or not sys.argv[2].isdigit():
        sys.exit(__doc__)

    command, user_id = sys.argv[1], sys.argv[2]
    public = load(PUBLIC, {"hashes": []})
    local = load(LOCAL, {})

    if command == "add":
        if user_id in local:
            sys.exit(f"User {user_id} already has a key. Revoke it first.")
        key = new_key()
        local[user_id] = key
        public["hashes"].append(key_hash(key, user_id))
        print(f"Key for {user_id} saved to {LOCAL.name}")
    else:
        if user_id not in local:
            sys.exit(f"No key found for user {user_id}.")
        public["hashes"].remove(key_hash(local.pop(user_id), user_id))
        print(f"Revoked key for {user_id}")

    save(PUBLIC, public)
    save(LOCAL, local)
    print("Now commit and push licenses.json.")


if __name__ == "__main__":
    main()
