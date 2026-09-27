#!/bin/sh
# Sends one error event with the Connect protocol (JSON over HTTP POST).
#
#   IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… ./examples/curl/send-event.sh
set -eu
: "${IZCI_URL:=http://localhost:5300}"
: "${IZCI_KEY:?set IZCI_KEY to an ingest key of the project (izk_…)}"

curl -sS --fail-with-body "$IZCI_URL/izci.ingest.v1.IngestService/SendEvent" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $IZCI_KEY" \
  --data @"$(dirname "$0")/../event.json"
echo
