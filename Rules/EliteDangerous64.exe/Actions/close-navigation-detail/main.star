LIST_OBSERVATION_ATTEMPTS = 12
LIST_OBSERVATION_INTERVAL_MS = 250
BACK_TRANSITION_SETTLE_MS = 200
AUTO_CLOSE_ATTEMPTS = 4
AUTO_CLOSE_CONFIRMATIONS_REQUIRED = 2

def normalize_text(text):
    normalized = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            normalized += character
    return normalized

def assist_detail_label_present(raw):
    for region in raw["regions"]:
        if region["detectionConfidence"] < 0.45 or region["recognitionConfidence"] < 0.60:
            continue
        if "SUPERCRUISEASSIST" in normalize_text(region["text"]):
            return True
    return False

def main(ctx):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
    # The Navigation list can exist for only one rendered frame before Elite
    # closes the panel after accepting Assist. Begin the first fresh
    # observation promptly enough to retain that transition evidence.
    task.sleep(milliseconds=BACK_TRANSITION_SETTLE_MS)

    navigation_seen = False
    for attempt in range(LIST_OBSERVATION_ATTEMPTS):
        observation = action.call(id="elite-dangerous/navigation-tab-transition-state", inputs={})
        state = observation["state"]
        if state == "NAVIGATION":
            navigation_seen = True
            break
        if attempt + 1 < LIST_OBSERVATION_ATTEMPTS:
            task.sleep(milliseconds=LIST_OBSERVATION_INTERVAL_MS)
    if not navigation_seen:
        if not ctx.inputs.get("detailLabelConfirmed", False):
            fail("Navigation detail BACK did not produce a confirmed NAVIGATION list transition")
        auto_closed_confirmations = 0
        for attempt in range(AUTO_CLOSE_ATTEMPTS):
            panel = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
            detail = action.call(id="elite-dangerous/lock-destination-text-regions", inputs={})
            if panel["activeTab"]["state"] == "ABSENT" and not assist_detail_label_present(detail):
                auto_closed_confirmations += 1
                if auto_closed_confirmations >= AUTO_CLOSE_CONFIRMATIONS_REQUIRED:
                    return {
                        "schemaVersion": 1,
                        "backSent": True,
                        "listConfirmed": False,
                        "autoClosedAfterBack": True,
                        "panelClosed": True,
                        "finalState": "ABSENT",
                    }
            else:
                auto_closed_confirmations = 0
            if attempt + 1 < AUTO_CLOSE_ATTEMPTS:
                task.sleep(milliseconds=LIST_OBSERVATION_INTERVAL_MS)
        fail("Navigation detail BACK did not produce a confirmed list or automatic forward-view transition")

    closed = action.call(id="elite-dangerous/close-left-panel", inputs={})
    if not closed["closed"] or closed["finalState"] != "ABSENT":
        fail("Navigation list did not close to the forward view")
    return {
        "schemaVersion": 1,
        "backSent": True,
        "listConfirmed": True,
        "autoClosedAfterBack": False,
        "panelClosed": closed["closed"],
        "finalState": closed["finalState"],
    }
