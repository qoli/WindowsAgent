UI_SETTLE_MS = 1000
OBSERVATION_SETTLE_MS = 250
MONITOR_POLL_MS = 500
STABLE_ATTEMPTS = 4
MAX_PANEL_CYCLES = 3
MAX_CONTACTS = 16
REQUEST_UNKNOWN_LIMIT = 4
REQUEST_CONFIRM_LIMIT = 12
REQUEST_CONFIRMATIONS = 2
AUTO_DOCK_START_LIMIT = 120
AUTO_DOCK_START_CONFIRMATIONS = 2
AUTO_DOCK_ACTIVE_LIMIT = 1800
AUTO_DOCK_MISSING_CONFIRMATIONS = 5
LANDING_GEAR_ON_CONFIRMATIONS = 2
LANDING_GEAR_UNKNOWN_LIMIT = 20
OBSERVATION_ERROR_LIMIT = 3

def emit_update(phase, sample, contact_index, range_state, distance_meters, request_state, flight_status, landing_gear, auto_dock_seen, auto_dock_consecutive, auto_dock_missing, landing_gear_on_consecutive, last_command=None, reason=None, observation_error_count=0, observation_error=None):
    stream.emit(
        type="action.dock-at-station.update",
        payload={
            "phase": phase,
            "sample": sample,
            "contactIndex": contact_index,
            "rangeState": range_state,
            "distanceMeters": distance_meters,
            "requestDocking": request_state,
            "flightStatus": flight_status,
            "landingGear": landing_gear,
            "autoDockSeen": auto_dock_seen,
            "autoDockConsecutive": auto_dock_consecutive,
            "autoDockMissingSamples": auto_dock_missing,
            "landingGearOnConsecutive": landing_gear_on_consecutive,
            "lastCommand": last_command,
            "reason": reason,
            "observationErrorCount": observation_error_count,
            "observationError": observation_error,
        },
    )

def observe_contacts_stable():
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/contacts-tab-state", inputs={})
        state = observation["contactsTab"]["state"]
        if state != "UNKNOWN" and state == previous:
            return observation
        previous = None if state == "UNKNOWN" else state
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Contacts tab did not produce two consecutive known observations")

def normalize_range_view(sample):
    contacts = observe_contacts_stable()
    state = contacts["contactsTab"]["state"]
    emit_update("NORMALIZING_RANGE_VIEW", sample, 0, None, None, None, None, None, False, 0, 0, 0, reason="CONTACTS_" + state)
    if state != "ABSENT":
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        emit_update("NORMALIZING_RANGE_VIEW", sample, 0, None, None, None, None, None, False, 0, 0, 0, last_command="FOCUS_LEFT_PANEL", reason="CLOSING_LEFT_PANEL")
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts = observe_contacts_stable()
        if contacts["contactsTab"]["state"] != "ABSENT":
            fail("left panel remained visible before request-docking-range Gate")
    task.sleep(milliseconds=UI_SETTLE_MS)

def open_contacts(sample, range_state, distance_meters):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    emit_update("OPENING_CONTACTS", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, last_command="FOCUS_LEFT_PANEL")
    task.sleep(milliseconds=UI_SETTLE_MS)
    contacts = observe_contacts_stable()
    state = contacts["contactsTab"]["state"]
    if state == "ABSENT":
        fail("left panel remained absent after FOCUS_LEFT_PANEL")
    for cycle in range(MAX_PANEL_CYCLES + 1):
        if state == "SELECTED":
            return
        if cycle == MAX_PANEL_CYCLES:
            break
        action.call(id="elite-dangerous/ui-control", inputs={"control": "NEXT_PANEL"})
        emit_update("OPENING_CONTACTS", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, last_command="NEXT_PANEL", reason="CONTACTS_" + state)
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts = observe_contacts_stable()
        state = contacts["contactsTab"]["state"]
        if state == "ABSENT":
            fail("left panel became absent while selecting CONTACTS")
    fail("CONTACTS was not reached within three NEXT_PANEL inputs")

def observe_request_known():
    last = None
    for attempt in range(REQUEST_UNKNOWN_LIMIT):
        last = action.call(id="elite-dangerous/request-docking-availability", inputs={})
        if last["requestDocking"]["state"] != "UNKNOWN":
            return last
        if attempt + 1 < REQUEST_UNKNOWN_LIMIT:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Request Docking state remained UNKNOWN")

def locate_and_focus_request(sample, range_state, distance_meters):
    for contact_index in range(MAX_CONTACTS):
        request = observe_request_known()
        state = request["requestDocking"]["state"]
        emit_update("LOCATING_REQUEST", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, reason=request["decision"]["reason"])
        if state == "DOCKING_ACTIVE":
            return {"contactIndex": contact_index, "alreadyActive": True}
        if state == "FOCUSED":
            return {"contactIndex": contact_index, "alreadyActive": False}
        if state == "AVAILABLE":
            action.call(id="elite-dangerous/ui-control", inputs={"control": "RIGHT"})
            emit_update("FOCUSING_REQUEST", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, last_command="RIGHT")
            task.sleep(milliseconds=UI_SETTLE_MS)
            focused = observe_request_known()
            focused_state = focused["requestDocking"]["state"]
            emit_update("FOCUSING_REQUEST", sample, contact_index, range_state, distance_meters, focused_state, None, None, False, 0, 0, 0, reason=focused["decision"]["reason"])
            if focused_state != "FOCUSED":
                fail("RIGHT did not produce FOCUSED Request Docking state")
            return {"contactIndex": contact_index, "alreadyActive": False}
        if state != "UNAVAILABLE":
            fail("unexpected Request Docking state while locating action: " + state)
        action.call(id="elite-dangerous/ui-control", inputs={"control": "DOWN"})
        emit_update("LOCATING_REQUEST", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, last_command="DOWN", reason="CURRENT_CONTACT_NOT_DOCKABLE")
        task.sleep(milliseconds=UI_SETTLE_MS)
    fail("Request Docking was not found within sixteen Contacts targets")

def request_and_verify(sample, contact_index, range_state, distance_meters, already_active):
    if already_active:
        emit_update("VERIFYING_GRANTED", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", None, None, False, 0, 0, 0, reason="CANCEL_DOCKING_ALREADY_PRESENT")
        return
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    emit_update("REQUESTING_DOCKING", sample, contact_index, range_state, distance_meters, "FOCUSED", None, None, False, 0, 0, 0, last_command="SELECT")
    confirmations = 0
    for attempt in range(REQUEST_CONFIRM_LIMIT):
        task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
        request = action.call(id="elite-dangerous/request-docking-availability", inputs={})
        state = request["requestDocking"]["state"]
        if state == "DOCKING_ACTIVE":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("VERIFYING_GRANTED", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, reason=request["decision"]["reason"])
        if confirmations >= REQUEST_CONFIRMATIONS:
            return
    fail("SELECT was not followed by two consecutive CANCEL DOCKING observations")

def close_panel(sample, contact_index, range_state, distance_meters):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    emit_update("CLOSING_PANEL", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", None, None, False, 0, 0, 0, last_command="FOCUS_LEFT_PANEL")
    task.sleep(milliseconds=UI_SETTLE_MS)
    contacts = observe_contacts_stable()
    if contacts["contactsTab"]["state"] != "ABSENT":
        fail("left panel remained visible after docking request")
    task.sleep(milliseconds=UI_SETTLE_MS)

def observe_flight_and_gear():
    raw_attempt = action.try_call(id="elite-dangerous/flight-prompt-text", inputs={})
    if not raw_attempt["ok"]:
        return {"ok": False, "error": raw_attempt["error"]}
    raw = raw_attempt["output"]
    flight_attempt = action.try_call(id="elite-dangerous/flight-status", inputs=raw)
    if not flight_attempt["ok"]:
        return {"ok": False, "error": flight_attempt["error"]}
    ship_attempt = action.try_call(id="elite-dangerous/ship-status", inputs={})
    if not ship_attempt["ok"]:
        return {"ok": False, "error": ship_attempt["error"]}
    flight = flight_attempt["output"]
    ship = ship_attempt["output"]
    return {
        "ok": True,
        "error": None,
        "flightStatus": flight["flightStatus"]["state"],
        "flightPromptText": raw["text"],
        "landingGear": ship["shipStatus"]["landingGear"]["state"],
    }

def main(ctx):
    sample = 0
    normalize_range_view(sample)
    range_observation = action.call(id="elite-dangerous/request-docking-range", inputs={})
    range_gate = range_observation["requestDockingRange"]
    range_state = range_gate["state"]
    distance_meters = range_gate["distanceMeters"]
    if range_state != "ALLOWED":
        fail("request-docking-range Gate must be ALLOWED, got " + range_state + ": " + range_gate["evidence"]["reason"])
    emit_update("RANGE_ADMITTED", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, reason="DISPLAY_DISTANCE_BELOW_THRESHOLD")

    open_contacts(sample, range_state, distance_meters)
    target = locate_and_focus_request(sample, range_state, distance_meters)
    contact_index = target["contactIndex"]
    request_and_verify(sample, contact_index, range_state, distance_meters, target["alreadyActive"])
    close_panel(sample, contact_index, range_state, distance_meters)

    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("HANDING_OVER", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", None, None, False, 0, 0, 0, last_command="SET_THROTTLE_0", reason=throttle["control"])

    auto_dock_consecutive = 0
    auto_dock_seen = False
    observation_errors = 0
    total_observation_errors = 0
    last_flight_status = "UNKNOWN"
    last_landing_gear = "UNKNOWN"
    for _ in range(AUTO_DOCK_START_LIMIT):
        observation = observe_flight_and_gear()
        sample += 1
        if not observation["ok"]:
            observation_errors += 1
            total_observation_errors += 1
            emit_update("OBSERVATION_ERROR", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", "UNKNOWN", "UNKNOWN", False, auto_dock_consecutive, 0, 0, reason="OBSERVATION_SAMPLE_FAILED", observation_error_count=observation_errors, observation_error=observation["error"])
            if observation_errors >= OBSERVATION_ERROR_LIMIT:
                fail("three consecutive Auto Dock observation samples failed: " + observation["error"])
            task.sleep(milliseconds=MONITOR_POLL_MS)
            continue
        observation_errors = 0
        last_flight_status = observation["flightStatus"]
        last_landing_gear = observation["landingGear"]
        if last_flight_status == "AUTO_DOCK":
            auto_dock_consecutive += 1
        else:
            auto_dock_consecutive = 0
        if last_flight_status not in ["UNKNOWN", "SLOW_DOWN_FOR_AUTO_DOCK", "AUTO_DOCK"]:
            fail("unexpected known flight status while awaiting Auto Dock: " + last_flight_status)
        phase = "AUTO_DOCK_WAIT"
        if last_flight_status == "SLOW_DOWN_FOR_AUTO_DOCK":
            phase = "SLOWING_FOR_AUTO_DOCK"
        emit_update(phase, sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", last_flight_status, last_landing_gear, False, auto_dock_consecutive, 0, 0, reason=observation["flightPromptText"])
        if auto_dock_consecutive >= AUTO_DOCK_START_CONFIRMATIONS:
            auto_dock_seen = True
            break
        task.sleep(milliseconds=MONITOR_POLL_MS)
    if not auto_dock_seen:
        fail("AUTO_DOCK was not confirmed before the start limit")

    auto_dock_missing = 0
    landing_gear_on = 0
    landing_gear_unknown = 0
    for _ in range(AUTO_DOCK_ACTIVE_LIMIT):
        observation = observe_flight_and_gear()
        sample += 1
        if not observation["ok"]:
            observation_errors += 1
            total_observation_errors += 1
            emit_update("OBSERVATION_ERROR", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", "UNKNOWN", "UNKNOWN", True, AUTO_DOCK_START_CONFIRMATIONS, auto_dock_missing, landing_gear_on, reason="OBSERVATION_SAMPLE_FAILED", observation_error_count=observation_errors, observation_error=observation["error"])
            if observation_errors >= OBSERVATION_ERROR_LIMIT:
                fail("three consecutive final-approach observation samples failed: " + observation["error"])
            task.sleep(milliseconds=MONITOR_POLL_MS)
            continue
        observation_errors = 0
        last_flight_status = observation["flightStatus"]
        last_landing_gear = observation["landingGear"]
        if last_flight_status == "AUTO_DOCK":
            auto_dock_missing = 0
        elif last_flight_status == "UNKNOWN":
            auto_dock_missing += 1
        else:
            fail("unexpected known flight status after Auto Dock became active: " + last_flight_status)

        if last_landing_gear == "ON":
            landing_gear_on += 1
            landing_gear_unknown = 0
        elif last_landing_gear == "OFF":
            landing_gear_on = 0
            landing_gear_unknown = 0
        else:
            landing_gear_on = 0
            landing_gear_unknown += 1
            if landing_gear_unknown >= LANDING_GEAR_UNKNOWN_LIMIT:
                fail("Landing Gear remained UNKNOWN during Auto Dock")

        phase = "AUTO_DOCK_ACTIVE"
        if auto_dock_missing > 0 or landing_gear_on > 0:
            phase = "FINAL_APPROACH"
        emit_update(phase, sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", last_flight_status, last_landing_gear, True, AUTO_DOCK_START_CONFIRMATIONS, auto_dock_missing, landing_gear_on, reason=observation["flightPromptText"])
        if auto_dock_missing >= AUTO_DOCK_MISSING_CONFIRMATIONS and landing_gear_on >= LANDING_GEAR_ON_CONFIRMATIONS:
            emit_update("VISUAL_CONFIRMATION_REQUIRED", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", last_flight_status, last_landing_gear, True, AUTO_DOCK_START_CONFIRMATIONS, auto_dock_missing, landing_gear_on, reason="AUTO_DOCK_ABSENT_AND_LANDING_GEAR_ON")
            return {
                "schemaVersion": 1,
                "task": "DOCK_AT_STATION",
                "completed": True,
                "finalPhase": "VISUAL_CONFIRMATION_REQUIRED",
                "rangeState": range_state,
                "admittedDistanceMeters": distance_meters,
                "contactIndex": contact_index,
                "dockingRequestConfirmed": True,
                "panelClosed": True,
                "commandedThrottle": 0,
                "autoDockSeen": True,
                "autoDockMissingSamples": auto_dock_missing,
                "finalFlightStatus": last_flight_status,
                "finalLandingGear": last_landing_gear,
                "landingGearOnConfirmations": landing_gear_on,
                "visualConfirmed": False,
                "sampleCount": sample,
                "observationErrorCount": total_observation_errors,
            }
        task.sleep(milliseconds=MONITOR_POLL_MS)
    fail("Auto Dock did not reach the visual confirmation Gate before the active limit")
