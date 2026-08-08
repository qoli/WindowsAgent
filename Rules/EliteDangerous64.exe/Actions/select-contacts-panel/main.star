SETTLE_MS = 1000
STABLE_SAMPLE_MS = 250
STABLE_ATTEMPTS = 4
MAX_CYCLES = 3

def emit_update(phase, observation, cycle_count, opened_panel, command=None):
    stream.emit(
        type="action.select-contacts-panel.update",
        payload={
            "phase": phase,
            "observation": observation,
            "cycleCount": cycle_count,
            "openedPanel": opened_panel,
            "command": command,
        },
    )

def state_key(observation):
    return observation["contactsTab"]["state"]

def observe_stable(phase, cycle_count, opened_panel):
    previous_key = None
    observation_count = 0
    for attempt in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/contacts-tab-state", inputs={})
        observation_count += 1
        emit_update(phase, observation, cycle_count, opened_panel)
        contacts = observation["contactsTab"]
        key = state_key(observation)
        if contacts["state"] != "UNKNOWN" and key == previous_key:
            return {"observation": observation, "count": observation_count}
        if contacts["state"] == "UNKNOWN":
            previous_key = None
        else:
            previous_key = key
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=STABLE_SAMPLE_MS)
    fail("Contacts tab state did not produce two consecutive known observations")

def main(ctx):
    stream.activity(message="Inspecting Contacts panel", level="info")
    opened_panel = False
    cycle_count = 0
    observation_count = 0
    stable = observe_stable("OBSERVING_INITIAL_STATE", cycle_count, opened_panel)
    observation = stable["observation"]
    observation_count += stable["count"]
    contacts = observation["contactsTab"]

    if contacts["state"] == "ABSENT":
        stream.activity(message="Opening left panel", level="info")
        emit_update("OPENING_LEFT_PANEL", observation, cycle_count, opened_panel, command="FOCUS_LEFT_PANEL")
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        opened_panel = True
        task.sleep(milliseconds=SETTLE_MS)
        stable = observe_stable("OPENING_LEFT_PANEL", cycle_count, opened_panel)
        observation = stable["observation"]
        observation_count += stable["count"]
        contacts = observation["contactsTab"]
        if contacts["state"] == "ABSENT":
            fail("Contacts scan region was still absent after FOCUS_LEFT_PANEL")

    if contacts["state"] == "UNKNOWN":
        fail("Contacts tab state is UNKNOWN")

    for _ in range(MAX_CYCLES):
        if contacts["state"] == "SELECTED":
            break
        stream.activity(message="Cycling to next panel", level="info")
        emit_update("CYCLING_PANEL", observation, cycle_count, opened_panel, command="NEXT_PANEL")
        action.call(id="elite-dangerous/ui-control", inputs={"control": "NEXT_PANEL"})
        cycle_count += 1
        task.sleep(milliseconds=SETTLE_MS)
        stable = observe_stable("VERIFYING_CONTACTS", cycle_count, opened_panel)
        observation = stable["observation"]
        observation_count += stable["count"]
        contacts = observation["contactsTab"]
        if contacts["state"] == "ABSENT":
            fail("Contacts scan region became absent after NEXT_PANEL")

    if contacts["state"] != "SELECTED":
        fail("CONTACTS was not reached within three NEXT_PANEL inputs")

    stream.activity(message="Contacts panel selected", level="info")
    emit_update("CONTACTS_SELECTED", observation, cycle_count, opened_panel)
    return {
        "schemaVersion": 1,
        "task": "SELECT_CONTACTS_PANEL",
        "completed": True,
        "finalPhase": "CONTACTS_SELECTED",
        "contactsState": "SELECTED",
        "selected": True,
        "openedPanel": opened_panel,
        "cycleCount": cycle_count,
        "observationCount": observation_count,
        "finalObservation": observation,
    }
