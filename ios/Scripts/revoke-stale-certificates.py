#!/usr/bin/env python3
"""Revokes the development certificates earlier CI runs made.

Each run signs on a fresh machine, so Xcode makes a new "Apple Development"
certificate through the App Store Connect API and its private key is lost
with the machine. Apple allows only a few, so the ones made by earlier runs
are revoked first. A certificate made in the last two hours is left alone:
another run may be signing with it. Only certificates named "Created via
API" are touched, never one made in Xcode on a person's own Mac.

Usage: revoke-stale-certificates.py KEY_ID ISSUER_ID KEY_PATH
"""

import base64
import datetime
import json
import subprocess
import sys
import time
import urllib.request

API = "https://api.appstoreconnect.apple.com/v1"


def encoded(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def token(key_id, issuer_id, key_path):
    header = encoded(json.dumps({"alg": "ES256", "kid": key_id, "typ": "JWT"}).encode())
    now = int(time.time())
    claims = encoded(json.dumps({"iss": issuer_id, "iat": now, "exp": now + 600, "aud": "appstoreconnect-v1"}).encode())
    signing = f"{header}.{claims}".encode()
    der = subprocess.run(["openssl", "dgst", "-sha256", "-sign", key_path], input=signing, capture_output=True, check=True).stdout
    # The signature comes as DER; a JWT wants r and s side by side.
    offset = 2 if der[1] < 0x80 else 3
    parts = []
    for _ in range(2):
        length = der[offset + 1]
        parts.append(int.from_bytes(der[offset + 2:offset + 2 + length], "big"))
        offset += 2 + length
    return f"{header}.{claims}.{encoded(parts[0].to_bytes(32, 'big') + parts[1].to_bytes(32, 'big'))}"


def call(method, path, bearer):
    request = urllib.request.Request(API + path, method=method, headers={"Authorization": f"Bearer {bearer}"})
    with urllib.request.urlopen(request) as response:
        body = response.read()
        return json.loads(body) if body else None


def main():
    bearer = token(*sys.argv[1:4])
    listed = call("GET", "/certificates?filter[certificateType]=DEVELOPMENT&limit=200", bearer)
    # A certificate lasts a year, so its expiry says when it was made.
    newest = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=365) - datetime.timedelta(hours=2)
    for certificate in listed["data"]:
        attributes = certificate["attributes"]
        expires = datetime.datetime.fromisoformat(attributes["expirationDate"].replace("Z", "+00:00"))
        if attributes.get("displayName") != "Created via API" or expires > newest:
            continue
        call("DELETE", f"/certificates/{certificate['id']}", bearer)
        print(f"revoked {certificate['id']}, which expired {attributes['expirationDate'][:10]}")


if __name__ == "__main__":
    main()
