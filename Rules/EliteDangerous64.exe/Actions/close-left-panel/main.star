SETTLE_MS = 1000
OBSERVATION_SETTLE_MS = 250
STABLE_ATTEMPTS = 6

def main(ctx):
    initial = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
    initial_state = initial["activeTab"]["state"]
    if initial_state == "UNKNOWN":
        fail("left panel state is UNKNOWN; cannot safely toggle it")
    if initial_state == "ABSENT":
        return {"schemaVersion": 1, "closed": True, "commandSent": False, "initialState": initial_state, "finalState": "ABSENT"}

    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    task.sleep(milliseconds=SETTLE_MS)
    absent_confirmations = 0
    for attempt in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
        state = observation["activeTab"]["state"]
        if state == "ABSENT":
            absent_confirmations += 1
            if absent_confirmations >= 2:
                return {"schemaVersion": 1, "closed": True, "commandSent": True, "initialState": initial_state, "finalState": "ABSENT"}
        elif state != "UNKNOWN":
            absent_confirmations = 0
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("left panel did not produce two confirmed ABSENT observations after FOCUS_LEFT_PANEL")
