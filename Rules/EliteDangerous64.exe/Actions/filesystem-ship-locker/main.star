CAPABILITY_ID = "elite-dangerous/filesystem/ship-locker"
FILE_NAME = "ShipLocker.json"
ALLOWED_EVENTS = ["ShipLocker"]
ALLOWED_FIELDS = ["timestamp","event","Items","Components","Consumables","Data"]
MAX_BYTES = 786432
CURRENT_MS = 15000
EXPIRED_FRESHNESS = "UNKNOWN"
UPDATE_MODE = "EVENT_SNAPSHOT"

def selected_fields(data):
    selected = {}
    for field in ALLOWED_FIELDS:
        if field in data:
            selected[field] = data[field]
    return selected

def classify_freshness(source_age_ms):
    if source_age_ms < 0:
        return "UNKNOWN"
    if source_age_ms <= CURRENT_MS:
        return "CURRENT"
    return EXPIRED_FRESHNESS

def main(ctx):
    result = observer.file.read_json(
        path = {"root": "elite-dangerous-journal", "relative": FILE_NAME},
        maxBytes = MAX_BYTES,
    )
    if not result["exists"]:
        return {
            "schemaVersion": 1,
            "state": "ABSENT",
            "freshness": "UNKNOWN",
            "source": {
                "kind": "elite-dangerous-filesystem",
                "capabilityId": CAPABILITY_ID,
                "file": FILE_NAME,
                "event": None,
                "updateMode": UPDATE_MODE,
                "observedAt": result["observedAt"],
                "sourceTimestamp": None,
                "fileModifiedAt": None,
                "sourceAgeMs": None,
                "fileAgeMs": None,
                "sizeBytes": None,
            },
            "data": None,
        }
    data = result["data"]
    if "event" not in data or data["event"] not in ALLOWED_EVENTS:
        return job.fail(
            code = "ED_FILESYSTEM_EVENT_INVALID",
            message = FILE_NAME + " does not contain an allowed event discriminator",
        )
    if "timestamp" not in data or result["sourceTimestamp"] == None or result["sourceTimestampAgeMs"] == None:
        return job.fail(
            code = "ED_FILESYSTEM_TIMESTAMP_INVALID",
            message = FILE_NAME + " does not contain a valid source timestamp",
        )
    return {
        "schemaVersion": 1,
        "state": "AVAILABLE",
        "freshness": classify_freshness(result["sourceTimestampAgeMs"]),
        "source": {
            "kind": "elite-dangerous-filesystem",
            "capabilityId": CAPABILITY_ID,
            "file": FILE_NAME,
            "event": data["event"],
            "updateMode": UPDATE_MODE,
            "observedAt": result["observedAt"],
            "sourceTimestamp": result["sourceTimestamp"],
            "fileModifiedAt": result["modifiedAt"],
            "sourceAgeMs": result["sourceTimestampAgeMs"],
            "fileAgeMs": result["modifiedAgeMs"],
            "sizeBytes": result["sizeBytes"],
        },
        "data": selected_fields(data),
    }
