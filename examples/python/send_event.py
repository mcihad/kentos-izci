"""Reports a caught exception to izci with the Connect protocol (JSON over
HTTP), using only the Python standard library.

    IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… python3 examples/python/send_event.py

Existing Python applications can also keep sentry-sdk and point its DSN at
izci (see docs/sentry.md).
"""

import json
import os
import platform
import traceback
import urllib.error
import urllib.request

URL = os.environ.get("IZCI_URL", "http://localhost:5300")
KEY = os.environ["IZCI_KEY"]


def exceptions(exc):
    """The chain root cause first; traceback frames are already oldest first."""
    chain = []
    while exc is not None:
        frames = [
            {
                "function": f.name,
                "filename": os.path.relpath(f.filename),
                "lineno": f.lineno,
                "contextLine": f.line or "",
                "inApp": "site-packages" not in f.filename,
            }
            for f in traceback.extract_tb(exc.__traceback__)
        ]
        chain.insert(0, {"type": type(exc).__name__, "value": str(exc), "stacktrace": {"frames": frames}})
        exc = exc.__cause__ or exc.__context__
    return chain


def report(exc, **extra):
    event = {
        "level": "LEVEL_ERROR",
        "platform": "python",
        "environment": "production",
        "release": "tahsilat@3.0.1",
        "contexts": {"runtime": {"name": "CPython", "version": platform.python_version()}},
        "exceptions": exceptions(exc),
        **extra,
    }
    req = urllib.request.Request(
        f"{URL}/izci.ingest.v1.IngestService/SendEvent",
        data=json.dumps({"event": event}).encode(),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {KEY}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as res:
            return json.load(res)["result"]
    except urllib.error.HTTPError as err:
        raise RuntimeError(f"izci answered {err.code}: {err.read().decode()}") from None


def tahsil_et(tutar):
    return 100 / tutar


try:
    tahsil_et(0)
except ZeroDivisionError as exc:
    result = report(exc, customer={"id": "amasya-belediyesi", "name": "Amasya Belediyesi"}, tags={"modul": "tahsilat"})
    print(result["outcome"], result["issueShortId"], result["issueUrl"])
