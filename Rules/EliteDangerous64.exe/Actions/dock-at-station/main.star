UI_SETTLE_MS = 1000
OBSERVATION_SETTLE_MS = 250
MONITOR_POLL_MS = 500
STABLE_ATTEMPTS = 4
PANEL_STABLE_ATTEMPTS = 8
PANEL_OBSERVATION_SETTLE_MS = 500
MAX_PANEL_CYCLES = 3
MAX_CONTACTS = 16
REQUEST_UNKNOWN_LIMIT = 4
REQUEST_CONFIRM_LIMIT = 12
REQUEST_CONFIRMATIONS = 2
REQUEST_SUBMIT_LIMIT = 3
AUTO_DOCK_START_LIMIT = 120
AUTO_DOCK_START_CONFIRMATIONS = 2
AUTO_DOCK_ACTIVE_LIMIT = 1800
AUTO_DOCK_MISSING_CONFIRMATIONS = 5
LANDING_GEAR_ON_CONFIRMATIONS = 2
LANDING_GEAR_UNKNOWN_LIMIT = 20
OBSERVATION_ERROR_LIMIT = 3
RANGE_POLL_MS = 1000
RANGE_ALLOWED_CONFIRMATIONS = 2
RANGE_OBSERVATION_ERROR_LIMIT = 5
RANGE_TREND_MIN_SAMPLES = 3
RANGE_MAX_STEP_METERS = 1000
SAFE_ADVANCE_THROTTLE_PERCENT = 75
SAFE_ADVANCE_STOP_METERS = 7000
SAFE_ADVANCE_MAX_DURATION_MS = 30000
AUTO_DOCK_LIFECYCLE_STATUSES = ["WAITING_IN_QUEUE", "SLOW_DOWN_FOR_AUTO_DOCK", "AUTO_DOCK"]

def emit_update(phase, sample, contact_index, range_state, distance_meters, request_state, flight_status, landing_gear, auto_dock_seen, auto_dock_consecutive, auto_dock_missing, landing_gear_on_consecutive, last_command=None, reason=None, observation_error_count=0, observation_error=None, range_wait_samples=0, range_trend_state="NOT_STARTED", range_trend_samples=0, accepted_distance_meters=None, range_outlier_count=0):
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
            "rangeWaitSamples": range_wait_samples,
            "rangeTrendState": range_trend_state,
            "rangeTrendSamples": range_trend_samples,
            "acceptedDistanceMeters": accepted_distance_meters,
            "rangeOutlierCount": range_outlier_count,
        },
    )

def observe_contacts_stable():
    previous = None
    for attempt in range(PANEL_STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
        state = observation["activeTab"]["state"]
        if state != "UNKNOWN" and state == previous:
            return observation
        previous = None if state == "UNKNOWN" else state
        if attempt + 1 < PANEL_STABLE_ATTEMPTS:
            task.sleep(milliseconds=PANEL_OBSERVATION_SETTLE_MS)
    fail("Contacts tab did not produce two consecutive known observations")

def normalize_range_view(sample):
    contacts = observe_contacts_stable()
    state = contacts["activeTab"]["state"]
    emit_update("NORMALIZING_RANGE_VIEW", sample, 0, None, None, None, None, None, False, 0, 0, 0, reason="CONTACTS_" + state)
    if state != "ABSENT":
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        emit_update("NORMALIZING_RANGE_VIEW", sample, 0, None, None, None, None, None, False, 0, 0, 0, last_command="FOCUS_LEFT_PANEL", reason="CLOSING_LEFT_PANEL")
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts = observe_contacts_stable()
        if contacts["activeTab"]["state"] != "ABSENT":
            fail("left panel remained visible before request-docking-range Gate")
    task.sleep(milliseconds=UI_SETTLE_MS)

def wait_for_range(sample):
    range_wait_samples = 0
    allowed_confirmations = 0
    observation_errors = 0
    baseline_candidate = None
    rebase_candidate = None
    accepted_distance = None
    trend_samples = 0
    outlier_count = 0
    safe_advance_used = False
    safe_advance_initial_distance = None
    safe_advance_final_distance = None
    while True:
        range_wait_samples += 1
        attempt = action.try_call(id="elite-dangerous/request-docking-range", inputs={})
        if not attempt["ok"]:
            observation_errors += 1
            allowed_confirmations = 0
            emit_update("RANGE_OBSERVATION_ERROR", sample, 0, None, None, None, None, None, False, 0, 0, 0, reason="RANGE_SAMPLE_FAILED", observation_error_count=observation_errors, observation_error=attempt["error"], range_wait_samples=range_wait_samples, range_trend_state="OBSERVATION_ERROR", range_trend_samples=trend_samples, accepted_distance_meters=accepted_distance, range_outlier_count=outlier_count)
            if observation_errors >= RANGE_OBSERVATION_ERROR_LIMIT:
                fail("five consecutive request-docking-range observations failed: " + attempt["error"])
            task.sleep(milliseconds=RANGE_POLL_MS)
            continue
        observation_errors = 0
        range_observation = attempt["output"]
        range_gate = range_observation["requestDockingRange"]
        range_state = range_gate["state"]
        distance_meters = range_gate["distanceMeters"]
        accepted_current = False
        trend_state = "SEEKING_BASELINE"
        if range_state in ["ALLOWED", "DENIED"] and distance_meters != None:
            if accepted_distance == None:
                if baseline_candidate == None:
                    baseline_candidate = distance_meters
                elif abs(distance_meters - baseline_candidate) <= RANGE_MAX_STEP_METERS:
                    accepted_distance = distance_meters
                    trend_samples = 2
                    baseline_candidate = None
                    accepted_current = True
                    trend_state = "TRACKING"
                else:
                    baseline_candidate = distance_meters
                    outlier_count += 1
                    trend_state = "OUTLIER_REJECTED"
            elif abs(distance_meters - accepted_distance) <= RANGE_MAX_STEP_METERS:
                accepted_distance = distance_meters
                trend_samples += 1
                rebase_candidate = None
                accepted_current = True
                trend_state = "TRACKING"
            else:
                outlier_count += 1
                trend_state = "OUTLIER_REJECTED"
                if rebase_candidate != None and abs(distance_meters - rebase_candidate) <= RANGE_MAX_STEP_METERS:
                    accepted_distance = distance_meters
                    trend_samples = 2
                    rebase_candidate = None
                    accepted_current = True
                    trend_state = "REBASED"
                else:
                    rebase_candidate = distance_meters
        else:
            trend_state = "EVIDENCE_UNKNOWN"
        trusted_range = accepted_current and trend_samples >= RANGE_TREND_MIN_SAMPLES
        if trusted_range and range_state == "ALLOWED":
            allowed_confirmations += 1
        else:
            allowed_confirmations = 0
        emit_update("RANGE_WAIT", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, reason=range_gate["evidence"]["reason"], range_wait_samples=range_wait_samples, range_trend_state=trend_state, range_trend_samples=trend_samples, accepted_distance_meters=accepted_distance, range_outlier_count=outlier_count)
        if allowed_confirmations >= RANGE_ALLOWED_CONFIRMATIONS:
            return {
                "rangeState": range_state,
                "distanceMeters": accepted_distance,
                "rangeWaitSamples": range_wait_samples,
                "rangeTrendSamples": trend_samples,
                "rangeOutlierCount": outlier_count,
                "safeAdvanceUsed": safe_advance_used,
                "safeAdvanceInitialDistanceMeters": safe_advance_initial_distance,
                "safeAdvanceFinalDistanceMeters": safe_advance_final_distance,
            }
        if trusted_range and range_state == "DENIED" and not safe_advance_used:
            safe_advance_used = True
            safe_advance_initial_distance = accepted_distance
            stream.activity(message="Safely advancing into docking range", level="info")
            emit_update("SAFE_ADVANCE_STARTED", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, last_command="ADVANCE_TOWARD_STATION", reason="TRUSTED_DISTANCE_OUTSIDE_DOCKING_RANGE", range_wait_samples=range_wait_samples, range_trend_state="TRACKING", range_trend_samples=trend_samples, accepted_distance_meters=accepted_distance, range_outlier_count=outlier_count)
            advance = action.call(
                id="elite-dangerous/advance-toward-station",
                inputs={
                    "throttlePercent": SAFE_ADVANCE_THROTTLE_PERCENT,
                    "stopAtStationDistanceMeters": SAFE_ADVANCE_STOP_METERS,
                    "maxDurationMs": SAFE_ADVANCE_MAX_DURATION_MS,
                },
            )
            if not advance["completed"] or advance["finalPhase"] != "STATION_DISTANCE_REACHED" or advance["finalStationDistanceMeters"] > SAFE_ADVANCE_STOP_METERS:
                fail("advance-toward-station returned an invalid completion result")
            safe_advance_final_distance = advance["finalStationDistanceMeters"]
            stream.activity(message="Safe docking-range advance completed", level="info")
            emit_update("SAFE_ADVANCE_COMPLETED", sample, 0, "ALLOWED", safe_advance_final_distance, None, None, None, False, 0, 0, 0, last_command="SET_THROTTLE_0", reason="STATION_DISTANCE_REACHED", range_wait_samples=range_wait_samples, range_trend_state="NOT_STARTED", range_trend_samples=0, accepted_distance_meters=None, range_outlier_count=outlier_count)
            allowed_confirmations = 0
            baseline_candidate = None
            rebase_candidate = None
            accepted_distance = None
            trend_samples = 0
        task.sleep(milliseconds=RANGE_POLL_MS)

def open_contacts(sample, range_state, distance_meters):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    emit_update("OPENING_CONTACTS", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, last_command="FOCUS_LEFT_PANEL")
    task.sleep(milliseconds=UI_SETTLE_MS)
    contacts = observe_contacts_stable()
    state = contacts["activeTab"]["state"]
    if state == "ABSENT":
        fail("left panel remained absent after FOCUS_LEFT_PANEL")
    for cycle in range(MAX_PANEL_CYCLES + 1):
        if state == "CONTACTS":
            return
        if cycle == MAX_PANEL_CYCLES:
            break
        action.call(id="elite-dangerous/ui-control", inputs={"control": "NEXT_PANEL"})
        emit_update("OPENING_CONTACTS", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, last_command="NEXT_PANEL", reason="CONTACTS_" + state)
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts = observe_contacts_stable()
        state = contacts["activeTab"]["state"]
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
    state = "FOCUSED"
    for submit_attempt in range(REQUEST_SUBMIT_LIMIT):
        if state == "AVAILABLE":
            action.call(id="elite-dangerous/ui-control", inputs={"control": "RIGHT"})
            emit_update("FOCUSING_REQUEST", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, last_command="RIGHT", reason="REFRESHING_REQUEST_FOCUS")
            task.sleep(milliseconds=UI_SETTLE_MS)
            focused = observe_request_known()
            state = focused["requestDocking"]["state"]
            emit_update("FOCUSING_REQUEST", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, reason=focused["decision"]["reason"])
        if state != "FOCUSED":
            fail("Request Docking submit retry could not prove focused state: " + state)

        action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
        emit_update("REQUESTING_DOCKING", sample, contact_index, range_state, distance_meters, "FOCUSED", None, None, False, 0, 0, 0, last_command="SELECT", reason="SUBMIT_ATTEMPT_" + str(submit_attempt + 1))
        confirmations = 0
        denial_observed = False
        request_return_confirmations = 0
        state = "UNKNOWN"
        for attempt in range(REQUEST_CONFIRM_LIMIT):
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
            request = action.call(id="elite-dangerous/request-docking-availability", inputs={})
            state = request["requestDocking"]["state"]
            phase = "VERIFYING_GRANTED"
            event_reason = request["decision"]["reason"]
            if state == "DENIED":
                denial_observed = True
                request_return_confirmations = 0
                phase = "REQUEST_DENIAL_PENDING"
            if state == "DOCKING_ACTIVE":
                confirmations += 1
                request_return_confirmations = 0
                if denial_observed:
                    phase = "REQUEST_DENIAL_OVERRIDDEN"
                    event_reason = "CANCEL_DOCKING_OVERRIDES_DENIAL_NOTIFICATION"
            else:
                confirmations = 0
            if denial_observed and state in ["AVAILABLE", "FOCUSED"]:
                request_return_confirmations += 1
            elif state not in ["DENIED", "DOCKING_ACTIVE"]:
                request_return_confirmations = 0
            emit_update(phase, sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, reason=event_reason)
            if confirmations >= REQUEST_CONFIRMATIONS:
                return
            if denial_observed and request_return_confirmations >= 2:
                emit_update("REQUEST_DENIED", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, reason="DENIAL_NOTIFICATION_FOLLOWED_BY_REQUEST_DOCKING")
                fail("DOCKING_REQUEST_DENIED: denial notification was followed by two Request Docking observations")
        if denial_observed:
            emit_update("REQUEST_DENIED", sample, contact_index, range_state, distance_meters, state, None, None, False, 0, 0, 0, reason="DENIAL_NOTIFICATION_WITHOUT_CANCEL_DOCKING")
            fail("DOCKING_REQUEST_DENIED: CANCEL DOCKING was not confirmed after the denial notification")
        if state not in ["AVAILABLE", "FOCUSED"]:
            fail("Request Docking submit did not return to a retryable state: " + state)
    fail("three focused SELECT attempts were not followed by two consecutive CANCEL DOCKING observations")

def close_panel(sample, contact_index, range_state, distance_meters):
    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    emit_update("CLOSING_PANEL", sample, contact_index, range_state, distance_meters, "DOCKING_ACTIVE", None, None, False, 0, 0, 0, last_command="FOCUS_LEFT_PANEL")
    task.sleep(milliseconds=UI_SETTLE_MS)
    contacts = observe_contacts_stable()
    if contacts["activeTab"]["state"] != "ABSENT":
        fail("left panel remained visible after docking request")
    task.sleep(milliseconds=UI_SETTLE_MS)

def observe_flight_and_gear():
    flight_attempt = action.try_call(id="elite-dangerous/flight-status", inputs={})
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
        "flightPromptText": flight["source"]["text"],
        "landingGear": ship["shipStatus"]["landingGear"]["state"],
    }

def main(ctx):
    stream.activity(message="Checking docking range", level="info")
    sample = 0
    normalize_range_view(sample)
    range_admission = wait_for_range(sample)
    range_state = range_admission["rangeState"]
    distance_meters = range_admission["distanceMeters"]
    range_wait_samples = range_admission["rangeWaitSamples"]
    range_trend_samples = range_admission["rangeTrendSamples"]
    range_outlier_count = range_admission["rangeOutlierCount"]
    safe_advance_used = range_admission["safeAdvanceUsed"]
    safe_advance_initial_distance = range_admission["safeAdvanceInitialDistanceMeters"]
    safe_advance_final_distance = range_admission["safeAdvanceFinalDistanceMeters"]
    stream.activity(message="Docking range confirmed", level="info")
    emit_update("RANGE_ADMITTED", sample, 0, range_state, distance_meters, None, None, None, False, 0, 0, 0, reason="TREND_CONFIRMED_WITH_TWO_ALLOWED_SAMPLES", range_wait_samples=range_wait_samples, range_trend_state="ADMITTED", range_trend_samples=range_trend_samples, accepted_distance_meters=distance_meters, range_outlier_count=range_outlier_count)

    stream.activity(message="Opening Contacts panel", level="info")
    action.on_failure(id="elite-dangerous/close-left-panel", inputs={})
    open_contacts(sample, range_state, distance_meters)
    target = locate_and_focus_request(sample, range_state, distance_meters)
    contact_index = target["contactIndex"]
    request_and_verify(sample, contact_index, range_state, distance_meters, target["alreadyActive"])
    stream.activity(message="Docking request confirmed", level="info")
    close_panel(sample, contact_index, range_state, distance_meters)
    action.clear_on_failure()

    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    stream.activity(message="Throttle set to 0%", level="info")
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
        if last_flight_status != "UNKNOWN" and last_flight_status not in AUTO_DOCK_LIFECYCLE_STATUSES:
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
    stream.activity(message="Auto Dock confirmed", level="info")

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
        if last_flight_status in AUTO_DOCK_LIFECYCLE_STATUSES:
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
            stream.activity(message="Visual docking confirmation required", level="warning")
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
                "rangeWaitSamples": range_wait_samples,
                "rangeTrendSamples": range_trend_samples,
                "rangeOutlierCount": range_outlier_count,
                "safeAdvanceUsed": safe_advance_used,
                "safeAdvanceInitialDistanceMeters": safe_advance_initial_distance,
                "safeAdvanceFinalDistanceMeters": safe_advance_final_distance,
            }
        task.sleep(milliseconds=MONITOR_POLL_MS)
    fail("Auto Dock did not reach the visual confirmation Gate before the active limit")
