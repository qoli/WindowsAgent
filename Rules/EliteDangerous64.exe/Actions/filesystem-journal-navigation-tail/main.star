ALLOWED_EVENTS = ["Location", "StartJump", "FSDJump", "FSDTarget"]
ALLOWED_FIELDS = ["timestamp", "event", "StarSystem", "SystemAddress", "JumpType", "RemainingJumpsInRoute"]

def selected_event(item):
    selected = {}
    for field in ALLOWED_FIELDS:
        selected[field] = item.get(field)
    return selected

def main(ctx):
    listing = observer.file.list(
        path={"root": "elite-dangerous-journal", "relative": "."},
        maxDepth=1,
        maxEntries=4096,
    )
    latest = None
    for entry in listing["entries"]:
        relative = entry["relative"]
        if entry["kind"] != "file" or not relative.startswith("Journal.") or not relative.endswith(".log"):
            continue
        if latest == None or entry["modifiedAt"] > latest["modifiedAt"]:
            latest = entry
    if latest == None:
        return {
            "schemaVersion": 1,
            "state": "ABSENT",
            "source": {"file": None, "observedAt": None, "modifiedAt": None, "sizeBytes": None, "offset": None},
            "events": [],
        }
    tail = observer.file.read_json_lines(
        path={"root": "elite-dangerous-journal", "relative": latest["relative"]},
        maxBytes=262144,
        maxLines=128,
    )
    events = []
    for item in tail["items"]:
        if item.get("event") in ALLOWED_EVENTS:
            events.append(selected_event(item))
    return {
        "schemaVersion": 1,
        "state": "AVAILABLE",
        "source": {
            "file": latest["relative"],
            "observedAt": tail["observedAt"],
            "modifiedAt": tail["modifiedAt"],
            "sizeBytes": tail["sizeBytes"],
            "offset": tail["offset"],
        },
        "events": events,
    }
