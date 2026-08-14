LIST_OBSERVATION_ATTEMPTS = 12
LIST_OBSERVATION_INTERVAL_MS = 250
BACK_TRANSITION_SETTLE_MS = 200

def main(ctx):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
    # The Navigation list can exist for only one rendered frame before Elite
    # closes the panel after accepting Assist. Begin the first fresh
    # observation promptly enough to retain that transition evidence.
    task.sleep(milliseconds=BACK_TRANSITION_SETTLE_MS)

    navigation_seen = False
    for attempt in range(LIST_OBSERVATION_ATTEMPTS):
        observation = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
        state = observation["activeTab"]["state"]
        if state == "NAVIGATION":
            navigation_seen = True
            break
        if attempt + 1 < LIST_OBSERVATION_ATTEMPTS:
            task.sleep(milliseconds=LIST_OBSERVATION_INTERVAL_MS)
    if not navigation_seen:
        fail("Navigation detail BACK did not produce a confirmed NAVIGATION list transition")

    closed = action.call(id="elite-dangerous/close-left-panel", inputs={})
    if not closed["closed"] or closed["finalState"] != "ABSENT":
        fail("Navigation list did not close to the forward view")
    return {
        "schemaVersion": 1,
        "backSent": True,
        "listConfirmed": True,
        "panelClosed": closed["closed"],
        "finalState": closed["finalState"],
    }
