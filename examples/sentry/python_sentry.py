"""An unmodified sentry-sdk application reporting to izci: only the DSN
points at izci. Customer and tenant travel as tags.

    pip install sentry-sdk
    IZCI_DSN=http://izk_…@localhost:5300/1 python3 examples/sentry/python_sentry.py
"""

import os

import sentry_sdk

sentry_sdk.init(
    dsn=os.environ["IZCI_DSN"],
    environment="production",
    release="tahsilat@3.0.1",
    traces_sample_rate=0,  # izci stores errors; transactions are dropped
)
sentry_sdk.set_user({"id": "3310"})
sentry_sdk.set_tag("customer", "sivas-belediyesi")
sentry_sdk.set_tag("customer_name", "Sivas Belediyesi")
sentry_sdk.set_tag("tenant", "sivas")


def borc_hesapla(abone):
    return abone["borc"] / abone["taksit"]


try:
    borc_hesapla({"borc": 1200, "taksit": 0})
except ZeroDivisionError as exc:
    print("event", sentry_sdk.capture_exception(exc))
sentry_sdk.flush()
