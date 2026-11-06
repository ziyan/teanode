#!/usr/bin/env python3
"""Print the newest TeaNode build testers can install now, as JSON.

Apple gives an app no way to ask this, and an upload is not it: a build is
processed and reviewed before testers see it, so what is read here is the
build's state in App Store Connect. Prints {} when there is none.

Reads APP_STORE_CONNECT_KEY_ID, APP_STORE_CONNECT_ISSUER_ID and
APP_STORE_CONNECT_KEY_PATH from the environment.
"""
import base64
import json
import os
import subprocess
import time
import urllib.parse
import urllib.request

BUNDLE_IDENTIFIER = "com.teanode.app"
ENDPOINT = "https://api.appstoreconnect.apple.com"
BUILDS_READ = 30


def encoded(raw):
    return base64.urlsafe_b64encode(raw).rstrip(b"=")


def der_integers(signature):
    """The two integers of a DER ECDSA signature, as 64 bytes."""
    position = 2 if signature[1] < 0x80 else 3
    numbers = []
    for _ in range(2):
        length = signature[position + 1]
        number = signature[position + 2 : position + 2 + length]
        numbers.append(number.lstrip(b"\0").rjust(32, b"\0"))
        position += 2 + length
    return b"".join(numbers)


def token():
    key_id = os.environ["APP_STORE_CONNECT_KEY_ID"]
    now = int(time.time())
    header = encoded(json.dumps({"alg": "ES256", "kid": key_id, "typ": "JWT"}).encode())
    payload = encoded(json.dumps({
        "iss": os.environ["APP_STORE_CONNECT_ISSUER_ID"],
        "iat": now, "exp": now + 600, "aud": "appstoreconnect-v1",
    }).encode())
    message = header + b"." + payload
    signature = subprocess.run(
        ["openssl", "dgst", "-sha256", "-sign", os.environ["APP_STORE_CONNECT_KEY_PATH"]],
        input=message, stdout=subprocess.PIPE, check=True,
    ).stdout
    return (message + b"." + encoded(der_integers(signature))).decode()


def read(path, parameters):
    url = ENDPOINT + path + "?" + urllib.parse.urlencode(parameters)
    request = urllib.request.Request(url, headers={"Authorization": "Bearer " + token()})
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def newest_with_testers(answer):
    """The newest build out of answer that testers can install, or None."""
    included = {(item["type"], item["id"]): item for item in answer.get("included", [])}

    def related(build, name):
        link = build["relationships"][name]["data"]
        return included.get((link["type"], link["id"])) if link else None

    for build in answer["data"]:
        attributes = build["attributes"]
        details = related(build, "buildBetaDetail")
        version = related(build, "preReleaseVersion")
        if attributes.get("expired") or attributes.get("processingState") != "VALID":
            continue
        if not details or not version:
            continue
        if details["attributes"].get("externalBuildState") == "IN_BETA_TESTING":
            return {"version": version["attributes"]["version"], "buildNumber": attributes["version"]}
    return None


def main():
    apps = read("/v1/apps", {"filter[bundleId]": BUNDLE_IDENTIFIER, "limit": 1})["data"]
    if not apps:
        print("{}")
        return
    app_id = apps[0]["id"]
    answer = read("/v1/builds", {
        "filter[app]": app_id,
        "sort": "-uploadedDate",
        "limit": BUILDS_READ,
        "include": "preReleaseVersion,buildBetaDetail",
    })
    newest = newest_with_testers(answer)
    if newest is None:
        print("{}")
        return
    newest["installUrl"] = "itms-beta://beta.itunes.apple.com/v1/app/" + app_id
    print(json.dumps(newest, sort_keys=True))


if __name__ == "__main__":
    main()
