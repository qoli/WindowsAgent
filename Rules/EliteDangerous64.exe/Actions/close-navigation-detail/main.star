LIST_CONFIRMATIONS = 2
LIST_OBSERVATION_ATTEMPTS = 6
LIST_OBSERVATION_INTERVAL_MS = 250

def main(ctx):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
    task.sleep(milliseconds=1000)

    navigation_confirmations = 0
    for attempt in range(LIST_OBSERVATION_ATTEMPTS):
        observation = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
        state = observation["activeTab"]["state"]
        if state == "NAVIGATION":
            navigation_confirmations += 1
            if navigation_confirmations >= LIST_CONFIRMATIONS:
                break
        else:
            navigation_confirmations = 0
        if attempt + 1 < LIST_OBSERVATION_ATTEMPTS:
            task.sleep(milliseconds=LIST_OBSERVATION_INTERVAL_MS)
    if navigation_confirmations < LIST_CONFIRMATIONS:
        fail("Navigation detail BACK did not produce two confirmed NAVIGATION list observations")

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
