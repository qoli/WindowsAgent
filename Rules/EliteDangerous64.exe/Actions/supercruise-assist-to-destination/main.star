UI_SETTLE_MS = 1000
POLL_MS = 500
STABLE_ATTEMPTS = 4
MAX_PANEL_CYCLES = 3
MAX_NAVIGATION = 8
NAVIGATION_LOCK_WARMUP_RETRIES = 3
SUPERCRUISE_ENTRY_LIMIT = 30
ASSIST_START_LIMIT = 240
BLUE_ZONE_GRACE_SAMPLES = 3
ASSIST_ALIGNMENT_CYCLE_LIMIT = 6
ALIGNMENT_PROMPT_CLEAR_CONFIRMATIONS = 2
ASSIST_ACTIVE_LIMIT = 2400
ASSIST_ACTIVE_CONFIRMATIONS = 2
ASSIST_MISSING_LIMIT = 30
STOPPED_CONFIRMATIONS = 3
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
MIN_TEXT_SIMILARITY = 0.72
FOCUSED_FILL_MINIMUM = 0.50
AVAILABLE_FILL_MAXIMUM = 0.15
LIST_MIN_Y = 320.0
LIST_MAX_Y = 760.0
LIST_MAX_X = 920.0

def emit_update(phase, sample, target_name, panel_tab=None, assist_button_state=None, flight_status="UNKNOWN", prompt_text=None, mass_lock=None, landing_gear=None, cargo_scoop=None, target=None, assist_active_confirmations=0, assist_missing_samples=0, stopped_confirmations=0, commanded_throttle=None, last_command=None, reason=None):
    stream.emit(
        type="action.supercruise-assist-to-destination.update",
        payload={
            "phase": phase,
            "sample": sample,
            "targetName": target_name,
            "panelTab": panel_tab,
            "assistButtonState": assist_button_state,
            "flightStatus": flight_status,
            "flightPromptText": prompt_text,
            "massLock": mass_lock,
            "landingGear": landing_gear,
            "cargoScoop": cargo_scoop,
            "targetDetected": None if target == None else target["detected"],
            "targetPresentation": None if target == None else target["presentation"],
            "targetOffsetX": None if target == None else target["offsetX"],
            "targetOffsetY": None if target == None else target["offsetY"],
            "targetCenterDistancePixels": None if target == None else target["centerDistancePixels"],
            "assistActiveConfirmations": assist_active_confirmations,
            "assistMissingSamples": assist_missing_samples,
            "stoppedConfirmations": stopped_confirmations,
            "commandedThrottle": commanded_throttle,
            "lastCommand": last_command,
            "reason": reason,
        },
    )

def normalize_text(text):
    normalized = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789<>":
            normalized += character
    return normalized

def edit_distance(left, right):
    previous = []
    for index in range(len(right) + 1):
        previous.append(index)
    for left_index in range(len(left)):
        current = [left_index + 1]
        for right_index in range(len(right)):
            insertion = current[right_index] + 1
            deletion = previous[right_index + 1] + 1
            substitution = previous[right_index]
            if left[left_index] != right[right_index]:
                substitution += 1
            best = insertion
            if deletion < best:
                best = deletion
            if substitution < best:
                best = substitution
            current.append(best)
        previous = current
    return previous[len(right)]

def similarity(left, right):
    maximum = len(left)
    if len(right) > maximum:
        maximum = len(right)
    if maximum == 0:
        return 0.0
    return 1.0 - float(edit_distance(left, right)) / float(maximum)

def bounds(region):
    minimum_x = region["referencePoints"][0]["x"]
    maximum_x = minimum_x
    minimum_y = region["referencePoints"][0]["y"]
    maximum_y = minimum_y
    for point in region["referencePoints"]:
        if point["x"] < minimum_x:
            minimum_x = point["x"]
        if point["x"] > maximum_x:
            maximum_x = point["x"]
        if point["y"] < minimum_y:
            minimum_y = point["y"]
        if point["y"] > maximum_y:
            maximum_y = point["y"]
    return {"left": minimum_x, "right": maximum_x, "centerY": (minimum_y + maximum_y) / 2.0}

def pixel_channels(pixel):
    return pixel // 65536, (pixel // 256) % 256, pixel % 256

def focus_fill_ratio(region):
    context = region["leftContext"]
    pixels = context["pixels"]
    expected = context["w"] * context["h"]
    if expected <= 0 or len(pixels) != expected:
        return None
    focused = 0
    inspected = 0
    for index in range(0, len(pixels), 8):
        red, green, blue = pixel_channels(pixels[index])
        inspected += 1
        if red >= 180 and green >= 70 and blue <= 100:
            focused += 1
    return float(focused) / float(inspected)

def observe_panel_stable():
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
        state = observation["activeTab"]["state"]
        if state != "UNKNOWN" and state == previous:
            return observation
        previous = None if state == "UNKNOWN" else state
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=POLL_MS)
    fail("left panel did not produce two consecutive known tab observations")

def open_navigation(target_name, sample):
    observation = observe_panel_stable()
    state = observation["activeTab"]["state"]
    emit_update("OPENING_NAVIGATION", sample, target_name, panel_tab=state, reason="CURRENT_PANEL_STATE")
    if state == "ABSENT":
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        emit_update("OPENING_NAVIGATION", sample, target_name, panel_tab=state, last_command="FOCUS_LEFT_PANEL", reason="OPENING_LEFT_PANEL")
        task.sleep(milliseconds=UI_SETTLE_MS)
        observation = observe_panel_stable()
        state = observation["activeTab"]["state"]
        if state == "ABSENT":
            fail("left panel remained absent after FOCUS_LEFT_PANEL")
    for cycle in range(MAX_PANEL_CYCLES + 1):
        if state == "NAVIGATION":
            return
        if cycle == MAX_PANEL_CYCLES:
            break
        action.call(id="elite-dangerous/ui-control", inputs={"control": "NEXT_PANEL"})
        emit_update("OPENING_NAVIGATION", sample, target_name, panel_tab=state, last_command="NEXT_PANEL", reason="CYCLING_TO_NAVIGATION")
        task.sleep(milliseconds=UI_SETTLE_MS)
        observation = observe_panel_stable()
        state = observation["activeTab"]["state"]
    fail("NAVIGATION was not reached within three NEXT_PANEL inputs")

def inspect_locked_target(raw, target_name):
    expected = normalize_text(target_name).replace("<", "").replace(">", "")
    best = None
    focused_rows = []
    for region in raw["regions"]:
        if len(region["referencePoints"]) != 4:
            continue
        box = bounds(region)
        if box["left"] >= LIST_MAX_X or box["centerY"] < LIST_MIN_Y or box["centerY"] > LIST_MAX_Y:
            continue
        normalized_with_brackets = normalize_text(region["text"])
        normalized = normalized_with_brackets.replace("<", "").replace(">", "")
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE or len(normalized) == 0:
            continue
        score = similarity(normalized, expected) * region["recognitionConfidence"]
        if best == None or score > best["score"]:
            best = {"region": region, "normalized": normalized_with_brackets, "similarity": similarity(normalized, expected), "score": score, "bounds": box}
        ratio = focus_fill_ratio(region)
        if ratio != None and ratio >= FOCUSED_FILL_MINIMUM:
            focused_rows.append({"centerY": box["centerY"], "text": region["text"]})
    if best == None or best["similarity"] < MIN_TEXT_SIMILARITY:
        return {"state": "UNKNOWN", "direction": None, "text": None if best == None else best["region"]["text"], "reason": "TARGET_TEXT_NOT_CONFIRMED"}
    if "<" not in best["normalized"] and ">" not in best["normalized"]:
        return {"state": "UNLOCKED", "direction": None, "text": best["region"]["text"], "reason": "NAVIGATION_BRACKETS_ABSENT"}
    ratio = focus_fill_ratio(best["region"])
    if ratio != None and ratio >= FOCUSED_FILL_MINIMUM:
        return {"state": "FOCUSED_LOCKED", "direction": None, "text": best["region"]["text"], "reason": "LOCKED_TARGET_FOCUSED"}
    if len(focused_rows) != 1:
        return {"state": "UNKNOWN", "direction": None, "text": best["region"]["text"], "reason": "FOCUSED_ROW_NOT_UNIQUE"}
    direction = "DOWN" if focused_rows[0]["centerY"] < best["bounds"]["centerY"] else "UP"
    return {"state": "VISIBLE_LOCKED", "direction": direction, "text": best["region"]["text"], "reason": "LOCKED_TARGET_DIRECTION_CONFIRMED"}

def observe_locked_target_stable(target_name, sample):
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        raw = action.call(id="elite-dangerous/navigation-list-text-regions", inputs={})
        observation = inspect_locked_target(raw, target_name)
        key = observation["state"] + ":" + str(observation["text"])
        emit_update("LOCATING_TARGET", sample, target_name, panel_tab="NAVIGATION", reason=observation["reason"])
        if observation["state"] != "UNKNOWN" and key == previous:
            return observation
        previous = None if observation["state"] == "UNKNOWN" else key
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=POLL_MS)
    fail("locked Navigation target did not produce two consecutive known observations")

def focus_and_open_target(target_name, sample):
    unlocked_warmup_count = 0
    for navigation_count in range(MAX_NAVIGATION + 1):
        observation = observe_locked_target_stable(target_name, sample)
        state = observation["state"]
        if state == "UNLOCKED":
            unlocked_warmup_count += 1
            emit_update(
                "LOCATING_TARGET",
                sample,
                target_name,
                panel_tab="NAVIGATION",
                reason="WAITING_FOR_NAVIGATION_LOCK_STABILIZATION_" + str(unlocked_warmup_count) + "_OF_" + str(NAVIGATION_LOCK_WARMUP_RETRIES),
            )
            if unlocked_warmup_count >= NAVIGATION_LOCK_WARMUP_RETRIES:
                fail("Navigation target remained visible but not locked after panel warm-up: " + target_name)
            task.sleep(milliseconds=UI_SETTLE_MS)
            continue
        unlocked_warmup_count = 0
        if state == "FOCUSED_LOCKED":
            action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
            emit_update("OPENING_DETAIL", sample, target_name, panel_tab="NAVIGATION", last_command="SELECT", reason="FOCUSED_LOCKED_TARGET")
            task.sleep(milliseconds=UI_SETTLE_MS)
            return
        if state != "VISIBLE_LOCKED" or observation["direction"] == None:
            fail("locked Navigation target focus direction is not confirmed")
        if navigation_count >= MAX_NAVIGATION:
            fail("locked Navigation target was not focused within eight directional inputs")
        action.call(id="elite-dangerous/ui-control", inputs={"control": observation["direction"]})
        emit_update("LOCATING_TARGET", sample, target_name, panel_tab="NAVIGATION", last_command=observation["direction"], reason=observation["reason"])
        task.sleep(milliseconds=UI_SETTLE_MS)

def inspect_assist_button(raw, destination_mode):
    best = None
    expected = "SUPERCRUISEASSISTANDORBIT" if destination_mode == "ORBIT_HANDOFF" else "SUPERCRUISEASSIST"
    for region in raw["regions"]:
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        normalized = normalize_text(region["text"]).replace("<", "").replace(">", "")
        state = "FOCUSED"
        label = normalized
        if normalized.startswith("DEACTIVATE"):
            state = "ACTIVE"
            label = normalized[len("DEACTIVATE"):]
        elif normalized.startswith("ACTIVATE"):
            label = normalized[len("ACTIVATE"):]
        score = similarity(label, expected)
        is_orbit = "ANDORBIT" in label
        if (destination_mode == "DROP" and is_orbit) or (destination_mode == "ORBIT_HANDOFF" and not is_orbit):
            score = 0.0
        candidate = {"region": region, "similarity": score, "score": score * region["recognitionConfidence"], "state": state}
        if best == None or candidate["score"] > best["score"]:
            best = candidate
    if best == None or best["similarity"] < MIN_TEXT_SIMILARITY:
        return {"state": "UNKNOWN", "text": None if best == None else best["region"]["text"], "focusFillRatio": None, "reason": "DROP_ASSIST_LABEL_NOT_CONFIRMED"}
    ratio = focus_fill_ratio(best["region"])
    # In the Navigation detail icon row the action label is contextual and is
    # rendered only for the focused icon. Its left context is outside the icon
    # fill, so it is retained as evidence but does not classify focus.
    if best["state"] == "ACTIVE":
        reason = "ORBIT_ASSIST_ALREADY_ACTIVE_CONFIRMED" if destination_mode == "ORBIT_HANDOFF" else "DROP_ASSIST_ALREADY_ACTIVE_CONFIRMED"
    else:
        reason = "ORBIT_ASSIST_CONTEXT_LABEL_CONFIRMED" if destination_mode == "ORBIT_HANDOFF" else "DROP_ASSIST_CONTEXT_LABEL_CONFIRMED"
    return {"state": best["state"], "text": best["region"]["text"], "focusFillRatio": ratio, "reason": reason}

def observe_assist_button_stable(target_name, sample, destination_mode):
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        raw = action.call(id="elite-dangerous/lock-destination-text-regions", inputs={})
        observation = inspect_assist_button(raw, destination_mode)
        key = observation["state"] + ":" + str(observation["text"])
        emit_update("LOCATING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state=observation["state"], reason=observation["reason"])
        if observation["state"] != "UNKNOWN" and key == previous:
            return observation
        previous = None if observation["state"] == "UNKNOWN" else key
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=POLL_MS)
    fail("Supercruise Assist button did not produce two consecutive known observations; the module may be absent or the detail layout is unsupported")

def request_assist(target_name, sample, destination_mode):
    # Detail action labels are contextual: with BACK focused the first action's
    # SUPERCRUISE ASSIST text is not rendered at all. Move focus exactly once,
    # then identify and validate the now-visible label before SELECT.
    action.call(id="elite-dangerous/ui-control", inputs={"control": "RIGHT"})
    emit_update("FOCUSING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state=None, last_command="RIGHT", reason="FOCUS_FIRST_DETAIL_ACTION_TO_REVEAL_LABEL")
    task.sleep(milliseconds=UI_SETTLE_MS)
    observation = observe_assist_button_stable(target_name, sample, destination_mode)
    if observation["state"] == "ACTIVE":
        emit_update("REQUESTING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state="ACTIVE", reason="ASSIST_ALREADY_ACTIVE_NO_SELECT")
        return
    if observation["state"] != "FOCUSED":
        fail("RIGHT did not produce a focused Supercruise Assist button")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    emit_update("REQUESTING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state="FOCUSED", last_command="SELECT", reason="ASSIST_ACTIVATION_REQUESTED")
    task.sleep(milliseconds=UI_SETTLE_MS)

def close_panel(target_name, sample):
    observation = observe_panel_stable()
    state = observation["activeTab"]["state"]
    if state != "ABSENT":
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        emit_update("CLOSING_PANEL", sample, target_name, panel_tab=state, last_command="FOCUS_LEFT_PANEL", reason="RESTORING_FORWARD_VIEW")
        task.sleep(milliseconds=UI_SETTLE_MS)
        observation = observe_panel_stable()
        state = observation["activeTab"]["state"]
    if state != "ABSENT":
        fail("left panel remained visible after requesting Supercruise Assist")

def observe_flight():
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    classified = action.call(id="elite-dangerous/flight-status", inputs=raw)
    return {"state": classified["flightStatus"]["state"], "text": raw["text"]}

def observe_supercruise_hud_stable():
    confirmations = 0
    last = None
    for _ in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/supercruise-hud-state", inputs={})
        last = observation
        if observation["supercruiseHud"]["state"] == "ACTIVE":
            confirmations += 1
            if confirmations >= 2:
                return observation
        else:
            confirmations = 0
        task.sleep(milliseconds=POLL_MS)
    fail("Supercruise HUD did not produce two consecutive ACTIVE observations")

def align_compass(target_name, control_profile):
    compass_result = action.call(
        id="elite-dangerous/align-station-target",
        inputs={
            "mode": "ALIGN",
            "targetMotion": "STATIC",
            "stopBeforeAlign": False,
            "controlProfile": control_profile,
        },
    )
    emit_update(
        "ALIGNING",
        compass_result["sampleCount"],
        target_name,
        last_command="ALIGN_STATION_TARGET",
        reason="SUPERVISED_STATIC_COMPASS_ALIGNMENT_COMPLETED:" + control_profile,
    )
    return compass_result["sampleCount"]

def align_visible_destination(target_name):
    visible_result = action.call(
        id="elite-dangerous/align-visible-target",
        inputs={
            "targetName": target_name,
            "stopBeforeAlign": False,
            "positionSource": "DESTINATION",
            "heatPolicy": "STRICT",
        },
    )
    emit_update(
        "ALIGNING",
        visible_result["sampleCount"],
        target_name,
        last_command="ALIGN_VISIBLE_TARGET",
        reason="VISIBLE_DESTINATION_FINE_ALIGNMENT_COMPLETED",
    )
    return visible_result["sampleCount"]

def resolve_assist_alignment_prompt(target_name, sample):
    for cycle_index in range(ASSIST_ALIGNMENT_CYCLE_LIMIT):
        cycle = cycle_index + 1
        compass_samples = align_compass(target_name, "SUPERCRUISE_ASSIST")
        visible_samples = align_visible_destination(target_name)
        emit_update(
            "CORRECTING_ASSIST_ALIGNMENT",
            sample,
            target_name,
            flight_status="FSD_ALIGNMENT_REQUIRED",
            last_command="ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET",
            reason="ASSIST_ALIGNMENT_CYCLE_COMPLETED:" + str(cycle) + ":" + str(compass_samples) + "+" + str(visible_samples),
        )

        clear_confirmations = 0
        active_confirmations = 0
        last_flight = None
        for _ in range(STABLE_ATTEMPTS):
            sample += 1
            last_flight = observe_flight()
            state = last_flight["state"]
            if state == "FSD_ALIGNMENT_REQUIRED":
                emit_update(
                    "VERIFYING_ASSIST_ALIGNMENT",
                    sample,
                    target_name,
                    flight_status=state,
                    prompt_text=last_flight["text"],
                    commanded_throttle=0,
                    reason="ALIGNMENT_PROMPT_STILL_PRESENT_AFTER_CYCLE:" + str(cycle),
                )
                break
            if state not in ["SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE", "SAFE_DISENGAGE_READY", "UNKNOWN"]:
                fail("unexpected known flight status while verifying Assist alignment: " + state)
            clear_confirmations += 1
            if state == "SUPERCRUISE_ASSIST_ACTIVE":
                active_confirmations += 1
            else:
                active_confirmations = 0
            emit_update(
                "VERIFYING_ASSIST_ALIGNMENT",
                sample,
                target_name,
                flight_status=state,
                prompt_text=last_flight["text"],
                assist_active_confirmations=active_confirmations,
                commanded_throttle=0,
                reason="ALIGNMENT_PROMPT_CLEAR_" + str(clear_confirmations) + "_OF_" + str(ALIGNMENT_PROMPT_CLEAR_CONFIRMATIONS) + ":CYCLE_" + str(cycle),
            )
            if clear_confirmations >= ALIGNMENT_PROMPT_CLEAR_CONFIRMATIONS:
                return {
                    "sample": sample,
                    "flight": last_flight,
                    "cycleCount": cycle,
                    "assistActiveConfirmations": active_confirmations,
                }
            task.sleep(milliseconds=POLL_MS)
    fail("ALIGNMENT_PROMPT_PERSISTED: Compass and visible-target alignment did not clear ALIGN WITH TARGET DESTINATION")

def preflight(target_name):
    ship = action.call(id="elite-dangerous/ship-status", inputs={})["shipStatus"]
    mass_lock = ship["massLock"]["state"]
    landing_gear = ship["landingGear"]["state"]
    cargo_scoop = ship["cargoScoop"]["state"]
    emit_update("PREFLIGHT", 0, target_name, mass_lock=mass_lock, landing_gear=landing_gear, cargo_scoop=cargo_scoop, reason="SHIP_STATUS_OBSERVED")
    if mass_lock != "OFF" or landing_gear != "OFF" or cargo_scoop != "OFF":
        fail("Supercruise Assist requires visual Mass Lock, Landing Gear, and Cargo Scoop all OFF")

def main(ctx):
    target_name = ctx.inputs["targetName"]
    destination_mode = ctx.inputs["destinationMode"]
    normal_space_confirmed = ctx.inputs.get("normalSpaceConfirmed", False)
    supercruise_confirmed = ctx.inputs.get("supercruiseConfirmed", False)
    assist_requested_confirmed = ctx.inputs.get("assistRequestedConfirmed", False)
    if not ctx.inputs["targetLocked"]:
        fail("targetLocked must be true after select-and-lock-destination completes")
    if normal_space_confirmed == supercruise_confirmed:
        fail("exactly one of normalSpaceConfirmed or supercruiseConfirmed must be true")
    if assist_requested_confirmed and not supercruise_confirmed:
        fail("assistRequestedConfirmed requires a confirmed existing Supercruise session")
    if not ctx.inputs["autoThrottleConfirmed"]:
        fail("autoThrottleConfirmed must be true for the game-controlled Assist contract")

    stream.activity(message="Checking Supercruise Assist preflight Gates", level="info")
    preflight(target_name)
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("PREFLIGHT", 0, target_name, commanded_throttle=0, last_command="SET_THROTTLE_0", reason=throttle["control"])
    stream.activity(message="Aligning destination before Supercruise Assist entry", level="info")
    initial_alignment_profile = "SUPERCRUISE_ASSIST" if supercruise_confirmed else "NORMAL_SPACE"
    sample = align_compass(target_name, initial_alignment_profile)

    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    charging_seen = supercruise_confirmed
    entered = supercruise_confirmed
    last_flight_status = "UNKNOWN"
    last_prompt_text = None
    if supercruise_confirmed:
        observe_supercruise_hud_stable()
        emit_update("ENTERED", sample, target_name, commanded_throttle=0, reason="SUPERCRUISE_HUD_CONFIRMED_AT_RESUME")
    else:
        fsd = action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
        throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
        emit_update("CHARGING", sample, target_name, commanded_throttle=100, last_command="SUPERCRUISE_TOGGLE", reason=fsd["control"] + "+" + throttle["control"])
        hud_confirmations = 0
        for _ in range(SUPERCRUISE_ENTRY_LIMIT):
            sample += 1
            flight = observe_flight()
            last_flight_status = flight["state"]
            last_prompt_text = flight["text"]
            target = None
            command = None
            phase = "CHARGING"
            if last_flight_status == "FSD_CHARGING":
                charging_seen = True
            elif last_flight_status == "FSD_THROTTLE_UP_REQUIRED":
                charging_seen = True
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
                command = "SET_THROTTLE_100"
                phase = "CHARGING"
            elif last_flight_status == "FSD_ALIGNMENT_REQUIRED":
                charging_seen = True
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                emit_update("ALIGNING_FOR_ENTRY", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="ALIGNMENT_REQUIRES_MINIMUM_THROTTLE:" + throttle["control"])
                alignment_sample_count = align_compass(target_name, "NORMAL_SPACE")
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
                command = "ALIGN_TARGETS:" + str(alignment_sample_count) + "+SET_THROTTLE_100"
                phase = "ALIGNING_FOR_ENTRY"
            elif last_flight_status == "SUPERCRUISE":
                if not charging_seen:
                    fail("SUPERCRUISE appeared without a preceding FSD charging observation")
                entered = True
                phase = "ENTERED"
            elif last_flight_status != "UNKNOWN":
                fail("unexpected known flight status before Supercruise entry: " + last_flight_status)
            if charging_seen and last_flight_status == "UNKNOWN":
                hud = action.call(id="elite-dangerous/supercruise-hud-state", inputs={})
                if hud["supercruiseHud"]["state"] == "ACTIVE":
                    hud_confirmations += 1
                else:
                    hud_confirmations = 0
                if hud_confirmations >= 2:
                    entered = True
                    phase = "ENTERED"
            emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, target=target, commanded_throttle=100, last_command=command, reason="WAITING_FOR_FSD_CHARGING_THEN_SUPERCRUISE_HUD")
            if entered:
                break
            task.sleep(milliseconds=POLL_MS)
        if not entered:
            fail("FSD charging followed by Supercruise entry was not visually confirmed")

    # Supercruise Assist is requested only after entry. This avoids the
    # version-dependent normal-space UI behavior where selecting Assist may
    # itself attempt to start FSD and race a later explicit toggle.
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("ENTERED", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, commanded_throttle=0, last_command="SET_THROTTLE_0", reason=throttle["control"])
    if not assist_requested_confirmed:
        action.on_failure(id="elite-dangerous/close-left-panel", inputs={}, timeout_milliseconds=10000)
        open_navigation(target_name, sample)
        focus_and_open_target(target_name, sample)
        action.on_failure(id="elite-dangerous/close-navigation-detail", inputs={}, timeout_milliseconds=10000)
        request_assist(target_name, sample, destination_mode)
        close_panel(target_name, sample)
        action.clear_on_failure()
        action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    else:
        emit_update("WAITING_FOR_ASSIST", sample, target_name, commanded_throttle=0, reason="ASSIST_REQUEST_CONFIRMED_AT_RESUME")

    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
    emit_update("WAITING_FOR_ASSIST", sample, target_name, commanded_throttle=75, last_command="SET_THROTTLE_75", reason="SUPERCRUISE_ASSIST_BLUE_ZONE_REQUESTED:" + throttle["control"])

    assist_active_confirmations = 0
    alignment_required_samples = 0
    for _ in range(ASSIST_START_LIMIT):
        sample += 1
        flight = observe_flight()
        last_flight_status = flight["state"]
        last_prompt_text = flight["text"]
        target = None
        command = None
        commanded_throttle = 75
        phase = "WAITING_FOR_ASSIST"
        if last_flight_status == "SUPERCRUISE_ASSIST_ACTIVE":
            assist_active_confirmations += 1
            alignment_required_samples = 0
            phase = "ASSIST_ACTIVE"
        elif last_flight_status == "FSD_ALIGNMENT_REQUIRED":
            assist_active_confirmations = 0
            alignment_required_samples += 1
            if alignment_required_samples > BLUE_ZONE_GRACE_SAMPLES:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                emit_update("CORRECTING_ASSIST_ALIGNMENT", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="ASSIST_ALIGNMENT_REQUIRES_MINIMUM_THROTTLE:" + throttle["control"])
                alignment = resolve_assist_alignment_prompt(target_name, sample)
                sample = alignment["sample"]
                last_flight_status = alignment["flight"]["state"]
                last_prompt_text = alignment["flight"]["text"]
                assist_active_confirmations = alignment["assistActiveConfirmations"]
                if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
                    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
                    command = "ALIGN_TARGETS:" + str(alignment["cycleCount"]) + "+SET_THROTTLE_75"
                    commanded_throttle = 75
                else:
                    command = "ALIGN_TARGETS:" + str(alignment["cycleCount"])
                    commanded_throttle = 0
                alignment_required_samples = 0
                phase = "ASSIST_ACTIVE" if assist_active_confirmations >= ASSIST_ACTIVE_CONFIRMATIONS else "VERIFYING_ASSIST_ALIGNMENT"
        elif last_flight_status in ["SUPERCRUISE", "SAFE_DISENGAGE_READY", "UNKNOWN"]:
            assist_active_confirmations = 0
            alignment_required_samples = 0
        else:
            fail("unexpected known flight status while awaiting Supercruise Assist activation: " + last_flight_status)
        emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, target=target, assist_active_confirmations=assist_active_confirmations, commanded_throttle=commanded_throttle, last_command=command, reason="WAITING_FOR_ALIGNMENT_PROMPT_CLEAR_THEN_BLUE_ZONE_ASSIST")
        if assist_active_confirmations >= ASSIST_ACTIVE_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
        fail("SUPERCRUISE ASSIST ACTIVE was not confirmed twice; Auto Throttle or Assist activation may not be active")

    if destination_mode == "ORBIT_HANDOFF":
        action.clear_on_failure()
        stream.activity(message="Supercruise Assist and Orbit owns flight controls", level="info")
        emit_update("ASSIST_ACTIVE", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, commanded_throttle=None, reason="ORBIT_ASSIST_HANDOFF_CONFIRMED")
        return {
            "schemaVersion": 1,
            "task": "SUPERCRUISE_ASSIST_TO_DESTINATION",
            "completed": True,
            "finalPhase": "ASSIST_HANDOFF",
            "targetName": target_name,
            "destinationMode": destination_mode,
            "assistActiveConfirmations": assist_active_confirmations,
            "assistMissingSamples": 0,
            "stoppedConfirmations": 0,
            "agentFlightInputAfterAssistActive": False,
            "finalSpeed": None,
            "sampleCount": sample,
        }

    stream.activity(message="Supercruise Assist owns flight controls", level="info")
    assist_missing_samples = 0
    stopped_confirmations = 0
    final_speed = None
    for _ in range(ASSIST_ACTIVE_LIMIT):
        sample += 1
        flight = observe_flight()
        speed = action.call(id="elite-dangerous/ship-speed", inputs={})["speed"]
        final_speed = speed
        last_flight_status = flight["state"]
        last_prompt_text = flight["text"]
        if last_flight_status in ["SUPERCRUISE_ASSIST_ACTIVE", "SAFE_DISENGAGE_READY"]:
            assist_missing_samples = 0
        elif last_flight_status in ["UNKNOWN", "SUPERCRUISE"]:
            assist_missing_samples += 1
        else:
            fail("unexpected known flight status while Supercruise Assist owns flight: " + last_flight_status)
        if speed["state"] == "STOPPED":
            stopped_confirmations += 1
        else:
            stopped_confirmations = 0
        phase = "VERIFYING_STOP" if assist_missing_samples > 0 else "GAME_CONTROLLED_APPROACH"
        emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, assist_missing_samples=assist_missing_samples, stopped_confirmations=stopped_confirmations, commanded_throttle=None, reason="NO_AGENT_FLIGHT_INPUT_AFTER_ASSIST_ACTIVE")
        if assist_missing_samples >= STOPPED_CONFIRMATIONS and stopped_confirmations >= STOPPED_CONFIRMATIONS:
            action.clear_on_failure()
            stream.activity(message="Supercruise Assist arrival confirmed", level="info")
            emit_update("COMPLETED", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, assist_missing_samples=assist_missing_samples, stopped_confirmations=stopped_confirmations, commanded_throttle=None, reason="ASSIST_AUTO_DROP_AND_STOP_CONFIRMED")
            return {
                "schemaVersion": 1,
                "task": "SUPERCRUISE_ASSIST_TO_DESTINATION",
                "completed": True,
                "finalPhase": "COMPLETED",
                "targetName": target_name,
                "destinationMode": destination_mode,
                "assistActiveConfirmations": assist_active_confirmations,
                "assistMissingSamples": assist_missing_samples,
                "stoppedConfirmations": stopped_confirmations,
                "agentFlightInputAfterAssistActive": False,
                "finalSpeed": final_speed,
                "sampleCount": sample,
            }
        if assist_missing_samples >= ASSIST_MISSING_LIMIT:
            fail("ASSIST_INTERRUPTED: Supercruise Assist prompt disappeared without a visually confirmed normal-space stop")
        task.sleep(milliseconds=POLL_MS)
    fail("Supercruise Assist did not reach automatic drop and stop before the active limit")
