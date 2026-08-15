UI_SETTLE_MS = 1000
POLL_MS = 500
STABLE_ATTEMPTS = 4
MAX_PANEL_CYCLES = 3
MAX_NAVIGATION = 8
NAVIGATION_LOCK_WARMUP_RETRIES = 3
SUPERCRUISE_ENTRY_LIMIT = 45
ASSIST_START_LIMIT = 240
ASSIST_START_NO_EVIDENCE_LIMIT = 6
BLUE_ZONE_GRACE_SAMPLES = 3
ASSIST_ALIGNMENT_CYCLE_LIMIT = 6
ALIGNMENT_PROMPT_CLEAR_CONFIRMATIONS = 2
ASSIST_ACTIVE_LIMIT = 2400
ASSIST_ACTIVE_CONFIRMATIONS = 2
ASSIST_MISSING_LIMIT = 30
LINE_OF_SIGHT_GATE_CONFIRMATIONS = 2
LINE_OF_SIGHT_REALIGN_CYCLE_LIMIT = 3
MAX_LINE_OF_SIGHT_RECOVERIES = 3
NEAR_ORBIT_CLEAR_CONFIRMATIONS = 2
NEAR_ORBIT_CLEAR_ATTEMPTS = 4
STOPPED_CONFIRMATIONS = 3
MAX_WGC_CAPTURE_ERRORS = 5
PREFLIGHT_STATUS_ATTEMPTS = 4
PREFLIGHT_SAFE_CONFIRMATIONS = 2
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
MIN_TEXT_SIMILARITY = 0.72
FOCUSED_FILL_MINIMUM = 0.50
AVAILABLE_FILL_MAXIMUM = 0.15
LIST_MIN_Y = 320.0
LIST_MAX_Y = 760.0
LIST_MAX_X = 920.0

def emit_update(phase, sample, target_name, panel_tab=None, assist_button_state=None, flight_status="UNKNOWN", flight_status_source=None, prompt_text=None, mass_lock=None, landing_gear=None, cargo_scoop=None, target=None, assist_active_confirmations=0, assist_missing_samples=0, line_of_sight_required_confirmations=0, line_of_sight_recovery_count=0, line_of_sight_control=None, stopped_confirmations=0, commanded_throttle=None, last_command=None, orbital_scale_state=None, orbital_scale_confidence=None, human_takeover_ready=False, reason=None):
    stream.emit(
        type="action.supercruise-assist-to-destination.update",
        payload={
            "phase": phase,
            "sample": sample,
            "targetName": target_name,
            "panelTab": panel_tab,
            "assistButtonState": assist_button_state,
            "flightStatus": flight_status,
            "flightStatusSource": flight_status_source,
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
            "lineOfSightRequiredConfirmations": line_of_sight_required_confirmations,
            "lineOfSightRecoveryCount": line_of_sight_recovery_count,
            "lineOfSightControl": line_of_sight_control,
            "stoppedConfirmations": stopped_confirmations,
            "commandedThrottle": commanded_throttle,
            "lastCommand": last_command,
            "orbitalScaleState": orbital_scale_state,
            "orbitalScaleConfidence": orbital_scale_confidence,
            "humanTakeoverReady": human_takeover_ready,
            "reason": reason,
        },
    )

def transient_wgc_region_capture_error(text):
    return "persistent WGC worker region capture" in text and "persistent region capture failed" in text

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

def observe_assist_button_stable(target_name, sample, destination_mode, flight_context):
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        raw = action.call(id="elite-dangerous/lock-destination-text-regions", inputs={})
        observation = inspect_assist_button(raw, destination_mode)
        key = observation["state"] + ":" + str(observation["text"])
        emit_update(
            "LOCATING_ASSIST",
            sample,
            target_name,
            panel_tab="NAVIGATION",
            assist_button_state=observation["state"],
            flight_status=flight_context["state"],
            flight_status_source="PRE_NAVIGATION_SNAPSHOT",
            prompt_text=flight_context["text"],
            reason=observation["reason"],
        )
        if observation["state"] != "UNKNOWN" and key == previous:
            return observation
        previous = None if observation["state"] == "UNKNOWN" else key
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=POLL_MS)
    fail("Supercruise Assist button did not produce two consecutive known observations; the module may be absent or the detail layout is unsupported")

def request_assist(target_name, sample, destination_mode, flight_context):
    # Detail action labels are contextual: with BACK focused the first action's
    # SUPERCRUISE ASSIST text is not rendered at all. Move focus exactly once,
    # then identify and validate the now-visible label before SELECT.
    action.call(id="elite-dangerous/ui-control", inputs={"control": "RIGHT"})
    emit_update("FOCUSING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state=None, flight_status=flight_context["state"], flight_status_source="PRE_NAVIGATION_SNAPSHOT", prompt_text=flight_context["text"], last_command="RIGHT", reason="FOCUS_FIRST_DETAIL_ACTION_TO_REVEAL_LABEL")
    task.sleep(milliseconds=UI_SETTLE_MS)
    observation = observe_assist_button_stable(target_name, sample, destination_mode, flight_context)
    if observation["state"] == "ACTIVE":
        emit_update("REQUESTING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state="ACTIVE", flight_status=flight_context["state"], flight_status_source="PRE_NAVIGATION_SNAPSHOT", prompt_text=flight_context["text"], reason="ASSIST_ALREADY_ACTIVE_NO_SELECT")
        return
    if observation["state"] != "FOCUSED":
        fail("RIGHT did not produce a focused Supercruise Assist button")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    emit_update("REQUESTING_ASSIST", sample, target_name, panel_tab="NAVIGATION", assist_button_state="FOCUSED", flight_status=flight_context["state"], flight_status_source="PRE_NAVIGATION_SNAPSHOT", prompt_text=flight_context["text"], last_command="SELECT", reason="ASSIST_ACTIVATION_REQUESTED")
    task.sleep(milliseconds=UI_SETTLE_MS)

def restore_forward_view(target_name, sample):
    # The Navigation detail page does not expose the tab-header pixels used by
    # left-panel-tab-state. Treating that detail view as ABSENT is therefore a
    # false close. Always leave detail through its owning Action, which sends
    # BACK, closes the returned list, and independently verifies ABSENT.
    restored = action.call(id="elite-dangerous/close-navigation-detail", inputs={"detailLabelConfirmed": True})
    if not restored["panelClosed"] or restored["finalState"] != "ABSENT":
        fail("Navigation detail did not restore the forward view after requesting Supercruise Assist")
    flight = observe_flight()
    line_of_sight_confirmations = 0
    if flight["state"] == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
        line_of_sight_confirmations = 1
    emit_update(
        "CLOSING_PANEL",
        sample,
        target_name,
        panel_tab="ABSENT",
        flight_status=flight["state"],
        flight_status_source="CURRENT_FRAME",
        prompt_text=flight["text"],
        line_of_sight_required_confirmations=line_of_sight_confirmations,
        commanded_throttle=0,
        last_command="BACK+FOCUS_LEFT_PANEL",
        reason="NAVIGATION_DETAIL_AND_PANEL_CLOSED_CONFIRMED",
    )
    return flight

def observe_flight():
    classified = action.call(id="elite-dangerous/flight-status", inputs={})
    return {"state": classified["flightStatus"]["state"], "text": classified["source"]["text"]}

def handoff_after_near_orbit(target_name, sample, gauge, reason):
    handoff = action.call(id="elite-dangerous/pause-at-exit-for-human-takeover", inputs={})
    ready = handoff["keyReplayCompleted"] and handoff["pauseSent"] and handoff["downCount"] == 5 and handoff["firstSelectSent"] and handoff["secondSelectSent"] and handoff["sequenceLength"] == 8 and not handoff["visualPostconditionClaimed"]
    if not ready:
        fail("NEAR_ORBIT_SAFE_EXIT_KEY_REPLAY_INCOMPLETE: throttle is 0% but the fixed safe-exit key sequence was incomplete")
    emit_update("HUMAN_TAKEOVER", sample, target_name, commanded_throttle=0, last_command="SAFE_EXIT_KEY_REPLAY", orbital_scale_state=gauge["state"], orbital_scale_confidence=gauge["confidence"], human_takeover_ready=ready, reason=reason + ":SAFE_EXIT_KEYS_REPLAYED_FOR_HUMAN_TAKEOVER")
    fail("NEAR_ORBIT_SAFETY_TRIGGERED: orbital scale remained detected after the bounded automatic sphere-separation attempt; throttle is 0% and the fixed safe-exit key sequence was replayed for human takeover; resulting menu state is not asserted")

def attempt_near_orbit_separation(target_name, sample, gauge, phase):
    attempt = action.try_call(id="elite-dangerous/fixed-supercruise-sphere-separation", inputs={})
    if not attempt["ok"]:
        emit_update("NEAR_ORBIT_AVOIDANCE_FAILED", sample, target_name, commanded_throttle=0, last_command="FIXED_SPHERE_SEPARATION", orbital_scale_state=gauge["state"], orbital_scale_confidence=gauge["confidence"], reason="NEAR_ORBIT_FIXED_SPHERE_SEPARATION_FAILED:" + str(attempt["errorCode"]))
        return {"cleared": False, "sample": sample, "control": None, "gauge": gauge, "reason": "NEAR_ORBIT_FIXED_SPHERE_SEPARATION_FAILED_DURING_" + phase}

    separation = attempt["output"]
    sample += separation["sampleCount"]
    control = separation["control"]
    emit_update("NEAR_ORBIT_AVOIDANCE", sample, target_name, commanded_throttle=0, last_command="FIXED_SPHERE_SEPARATION", orbital_scale_state=gauge["state"], orbital_scale_confidence=gauge["confidence"], line_of_sight_control=control, reason="NEAR_ORBIT_FIXED_SPHERE_SEPARATION_COMPLETED:6400+30000MS")

    confirmations = 0
    last_gauge = gauge
    for attempt_index in range(NEAR_ORBIT_CLEAR_ATTEMPTS):
        sample += 1
        last_gauge = action.call(id="elite-dangerous/orbital-scale-gauge-state", inputs={})["gauge"]
        if last_gauge["state"] == "ABSENT":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("VERIFYING_NEAR_ORBIT_CLEAR", sample, target_name, commanded_throttle=0, last_command=None, orbital_scale_state=last_gauge["state"], orbital_scale_confidence=last_gauge["confidence"], line_of_sight_control=control, reason="POST_SEPARATION_ORBITAL_SCALE_ABSENT_" + str(confirmations) + "_OF_" + str(NEAR_ORBIT_CLEAR_CONFIRMATIONS))
        if confirmations >= NEAR_ORBIT_CLEAR_CONFIRMATIONS:
            emit_update("NEAR_ORBIT_AVOIDANCE_COMPLETED", sample, target_name, commanded_throttle=0, last_command="FIXED_SPHERE_SEPARATION", orbital_scale_state=last_gauge["state"], orbital_scale_confidence=last_gauge["confidence"], line_of_sight_control=control, reason="POST_SEPARATION_ORBITAL_SCALE_CLEARED_DURING_" + phase)
            return {"cleared": True, "sample": sample, "control": control, "gauge": last_gauge, "reason": "POST_SEPARATION_ORBITAL_SCALE_CLEARED_DURING_" + phase}
        if attempt_index + 1 < NEAR_ORBIT_CLEAR_ATTEMPTS:
            task.sleep(milliseconds=POLL_MS)

    return {"cleared": False, "sample": sample, "control": control, "gauge": last_gauge, "reason": "NEAR_ORBIT_SCALE_PERSISTED_AFTER_FIXED_SPHERE_SEPARATION_DURING_" + phase}

def guard_near_orbit(target_name, sample, phase):
    gauge = action.call(id="elite-dangerous/orbital-scale-gauge-state", inputs={})["gauge"]
    if gauge["state"] != "DETECTED":
        return gauge
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("NEAR_ORBIT_SAFETY_TRIGGERED", sample, target_name, commanded_throttle=0, last_command="SET_THROTTLE_0", orbital_scale_state=gauge["state"], orbital_scale_confidence=gauge["confidence"], reason="ORBITAL_SCALE_DETECTED_DURING_" + phase + ":" + throttle["control"])
    recovered = attempt_near_orbit_separation(target_name, sample, gauge, phase)
    if recovered["cleared"]:
        return recovered["gauge"]
    handoff_after_near_orbit(target_name, recovered["sample"], recovered["gauge"], recovered["reason"])

def recover_near_orbit_or_handoff(target_name, sample):
    gauge = action.call(id="elite-dangerous/orbital-scale-gauge-state", inputs={})["gauge"]
    if gauge["state"] != "DETECTED":
        return {"recovered": False, "sample": sample, "control": None}
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("NEAR_ORBIT_SAFETY_TRIGGERED", sample, target_name, commanded_throttle=0, last_command="SET_THROTTLE_0", orbital_scale_state=gauge["state"], orbital_scale_confidence=gauge["confidence"], reason="ORBITAL_SCALE_DETECTED_DURING_GAME_CONTROLLED_APPROACH:" + throttle["control"])
    recovered = attempt_near_orbit_separation(target_name, sample, gauge, "GAME_CONTROLLED_APPROACH")
    if not recovered["cleared"]:
        handoff_after_near_orbit(target_name, recovered["sample"], recovered["gauge"], recovered["reason"])
    alignment = resolve_assist_alignment_prompt(target_name, recovered["sample"])
    restored = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
    emit_update("REACQUIRING_ASSIST", alignment["sample"], target_name, flight_status=alignment["flight"]["state"], prompt_text=alignment["flight"]["text"], assist_active_confirmations=alignment["assistActiveConfirmations"], commanded_throttle=75, last_command="SET_THROTTLE_75", orbital_scale_state=recovered["gauge"]["state"], orbital_scale_confidence=recovered["gauge"]["confidence"], line_of_sight_control=recovered["control"], reason="NEAR_ORBIT_AVOIDANCE_ALIGNMENT_CLEAR_RESTORING_BLUE_ZONE:" + restored["control"])
    return {"recovered": True, "sample": alignment["sample"], "control": recovered["control"], "flight": alignment["flight"], "assistActiveConfirmations": alignment["assistActiveConfirmations"]}

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
    alignment_purpose = "VISIBLE_HANDOFF" if control_profile == "SUPERCRUISE_ASSIST" else "HYPERSPACE_CHARGE"
    compass_result = action.call(
        id="elite-dangerous/align-station-target",
        inputs={
            "targetName": target_name,
            "mode": "ALIGN",
            "targetMotion": "STATIC",
            "alignmentPurpose": alignment_purpose,
            "stopBeforeAlign": False,
            "controlProfile": control_profile,
        },
    )
    emit_update(
        "ALIGNING",
        compass_result["sampleCount"],
        target_name,
        last_command="ALIGN_STATION_TARGET",
        reason="SUPERVISED_COMPASS_ALIGNMENT_COMPLETED:" + compass_result["targetMotion"] + ":" + control_profile + ":" + alignment_purpose,
    )
    return compass_result["sampleCount"]

def align_visible_destination(target_name):
    visible_result = action.call(
        id="elite-dangerous/align-visible-target",
        inputs={
            "targetName": target_name,
            "stopBeforeAlign": False,
            "centerHintConfirmed": True,
            "confirmedHintProfile": "SUPERCRUISE_ASSIST",
            "blueZoneGateEnabled": True,
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

def align_destination_pair(target_name, control_profile, phase, reason_prefix):
    guard_near_orbit(target_name, 0, phase + "_BEFORE_COMPASS")
    compass_samples = align_compass(target_name, control_profile)
    guard_near_orbit(target_name, compass_samples, phase + "_BEFORE_VISIBLE")
    visible_samples = align_visible_destination(target_name)
    guard_near_orbit(target_name, compass_samples + visible_samples, phase + "_AFTER_VISIBLE")
    emit_update(
        phase,
        compass_samples + visible_samples,
        target_name,
        commanded_throttle=0,
        last_command="ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET",
        reason=reason_prefix + ":" + str(compass_samples) + "+" + str(visible_samples),
    )
    return {"compassSamples": compass_samples, "visibleSamples": visible_samples}

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
            guard_near_orbit(target_name, sample, "VERIFYING_ASSIST_ALIGNMENT")
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

def resolve_assist_line_of_sight_prompt(target_name, sample, recovery_count):
    for cycle_index in range(LINE_OF_SIGHT_REALIGN_CYCLE_LIMIT):
        if recovery_count >= MAX_LINE_OF_SIGHT_RECOVERIES:
            fail("LINE_OF_SIGHT_RECOVERY_LIMIT: Supercruise Assist obstruction recurred more than three times")
        cycle = cycle_index + 1
        stream.activity(message="Clearing Supercruise Assist line-of-sight obstruction", level="info")
        cleared = action.call(
            id="elite-dangerous/clear-supercruise-assist-line-of-sight",
            inputs={"targetName": target_name},
        )
        sample += cleared["sampleCount"]
        recovery_count += 1
        control = cleared["control"]
        emit_update(
            "CLEARING_LINE_OF_SIGHT",
            sample,
            target_name,
            flight_status=cleared["finalFlightStatus"],
            line_of_sight_recovery_count=recovery_count,
            line_of_sight_control=control,
            commanded_throttle=0,
            last_command="CLEAR_LINE_OF_SIGHT",
            reason="LINE_OF_SIGHT_CHILD_COMPLETED_CYCLE_" + str(cycle),
        )

        guard_near_orbit(target_name, sample, "POST_LINE_OF_SIGHT_SEPARATION")

        emit_update(
            "REALIGNING_COMPASS_AFTER_SEPARATION",
            sample,
            target_name,
            line_of_sight_recovery_count=recovery_count,
            line_of_sight_control=control,
            commanded_throttle=0,
            last_command="ALIGN_STATION_TARGET",
            reason="SPHERE_EXIT_AND_30_SECOND_SEPARATION_COMPLETED:START_COMPASS_HANDOFF",
        )
        compass_samples = align_compass(target_name, "SUPERCRUISE_ASSIST")
        emit_update(
            "COMPASS_HANDOFF_CONFIRMED",
            sample,
            target_name,
            line_of_sight_recovery_count=recovery_count,
            line_of_sight_control=control,
            commanded_throttle=0,
            last_command="ALIGN_STATION_TARGET",
            reason="COMPASS_COARSE_ALIGNMENT_COMPLETED:" + str(compass_samples),
        )
        emit_update(
            "REALIGNING_VISIBLE_TARGET_AFTER_SEPARATION",
            sample,
            target_name,
            line_of_sight_recovery_count=recovery_count,
            line_of_sight_control=control,
            commanded_throttle=0,
            last_command="ALIGN_VISIBLE_TARGET",
            reason="START_VISIBLE_FOCUS_FRAME_FINE_ALIGNMENT",
        )
        visible_samples = align_visible_destination(target_name)
        emit_update(
            "REALIGNING_AFTER_LINE_OF_SIGHT",
            sample,
            target_name,
            line_of_sight_recovery_count=recovery_count,
            line_of_sight_control=control,
            commanded_throttle=0,
            last_command="ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET",
            reason="POST_SEPARATION_REALIGNMENT_COMPLETED:" + str(compass_samples) + "+" + str(visible_samples),
        )

        clear_confirmations = 0
        for _ in range(STABLE_ATTEMPTS):
            sample += 1
            guard_near_orbit(target_name, sample, "VERIFYING_LINE_OF_SIGHT_CLEAR")
            flight = observe_flight()
            state = flight["state"]
            if state == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
                emit_update(
                    "VERIFYING_LINE_OF_SIGHT_CLEAR",
                    sample,
                    target_name,
                    flight_status=state,
                    prompt_text=flight["text"],
                    line_of_sight_recovery_count=recovery_count,
                    line_of_sight_control=control,
                    commanded_throttle=0,
                    reason="LINE_OF_SIGHT_PROMPT_RETURNED_AFTER_REALIGNMENT:CYCLE_" + str(cycle),
                )
                break
            if state == "FSD_ALIGNMENT_REQUIRED":
                alignment = resolve_assist_alignment_prompt(target_name, sample)
                return {
                    "sample": alignment["sample"],
                    "flight": alignment["flight"],
                    "assistActiveConfirmations": alignment["assistActiveConfirmations"],
                    "recoveryCount": recovery_count,
                    "control": control,
                }
            if state not in ["SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE", "SAFE_DISENGAGE_READY", "UNKNOWN"]:
                fail("unexpected known flight status after clearing Assist line of sight: " + state)
            if state == "UNKNOWN":
                clear_confirmations = 0
            else:
                clear_confirmations += 1
            active_confirmations = clear_confirmations if state == "SUPERCRUISE_ASSIST_ACTIVE" else 0
            emit_update(
                "VERIFYING_LINE_OF_SIGHT_CLEAR",
                sample,
                target_name,
                flight_status=state,
                prompt_text=flight["text"],
                assist_active_confirmations=active_confirmations,
                line_of_sight_recovery_count=recovery_count,
                line_of_sight_control=control,
                commanded_throttle=0,
                reason="LINE_OF_SIGHT_PROMPT_CLEAR_" + str(clear_confirmations) + "_OF_" + str(ALIGNMENT_PROMPT_CLEAR_CONFIRMATIONS) + ":CYCLE_" + str(cycle),
            )
            if clear_confirmations >= ALIGNMENT_PROMPT_CLEAR_CONFIRMATIONS:
                return {
                    "sample": sample,
                    "flight": flight,
                    "assistActiveConfirmations": active_confirmations,
                    "recoveryCount": recovery_count,
                    "control": control,
                }
            task.sleep(milliseconds=POLL_MS)
    fail("LINE_OF_SIGHT_PROMPT_PERSISTED_AFTER_REALIGNMENT: bounded bypass cycles exhausted")

def preflight(target_name):
    safe_confirmations = 0
    for attempt in range(PREFLIGHT_STATUS_ATTEMPTS):
        ship = action.call(id="elite-dangerous/ship-status", inputs={})["shipStatus"]
        mass_lock = ship["massLock"]["state"]
        landing_gear = ship["landingGear"]["state"]
        cargo_scoop = ship["cargoScoop"]["state"]
        if mass_lock == "ON" or landing_gear == "ON" or cargo_scoop == "ON":
            emit_update("PREFLIGHT", attempt + 1, target_name, mass_lock=mass_lock, landing_gear=landing_gear, cargo_scoop=cargo_scoop, reason="SHIP_STATUS_UNSAFE")
            fail("Supercruise Assist requires visual Mass Lock, Landing Gear, and Cargo Scoop all OFF")
        if mass_lock == "OFF" and landing_gear == "OFF" and cargo_scoop == "OFF":
            safe_confirmations += 1
            emit_update("PREFLIGHT", attempt + 1, target_name, mass_lock=mass_lock, landing_gear=landing_gear, cargo_scoop=cargo_scoop, reason="SHIP_STATUS_SAFE_" + str(safe_confirmations) + "_OF_" + str(PREFLIGHT_SAFE_CONFIRMATIONS))
            if safe_confirmations >= PREFLIGHT_SAFE_CONFIRMATIONS:
                return
        else:
            safe_confirmations = 0
            emit_update("PREFLIGHT", attempt + 1, target_name, mass_lock=mass_lock, landing_gear=landing_gear, cargo_scoop=cargo_scoop, reason="SHIP_STATUS_INCOMPLETE_ATTEMPT_" + str(attempt + 1) + "_OF_" + str(PREFLIGHT_STATUS_ATTEMPTS))
        if attempt + 1 < PREFLIGHT_STATUS_ATTEMPTS:
            task.sleep(milliseconds=POLL_MS)
    fail("Supercruise Assist preflight ship status remained UNKNOWN within the bounded observation window")

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
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    guard_near_orbit(target_name, 0, "PREFLIGHT")
    stream.activity(message="Aligning destination before Supercruise Assist entry", level="info")
    initial_alignment_profile = "SUPERCRUISE_ASSIST" if supercruise_confirmed else "NORMAL_SPACE"
    initial_alignment = align_destination_pair(target_name, initial_alignment_profile, "ALIGNING_FOR_ENTRY", "INITIAL_ALIGNMENT_PAIR_COMPLETED")
    sample = initial_alignment["compassSamples"] + initial_alignment["visibleSamples"]

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
            guard_near_orbit(target_name, sample, "SUPERCRUISE_ENTRY")
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
                entry_alignment = align_destination_pair(target_name, "NORMAL_SPACE", "ALIGNING_FOR_ENTRY", "CHARGING_ALIGNMENT_PAIR_COMPLETED")
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
                command = "ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET:" + str(entry_alignment["compassSamples"]) + "+" + str(entry_alignment["visibleSamples"]) + "+SET_THROTTLE_100"
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
    guard_near_orbit(target_name, sample, "ENTERED")
    if not assist_requested_confirmed:
        action.on_failure(id="elite-dangerous/close-left-panel", inputs={}, timeout_milliseconds=10000)
        ui_flight_context = observe_flight()
        open_navigation(target_name, sample)
        focus_and_open_target(target_name, sample)
        action.on_failure(id="elite-dangerous/close-navigation-detail", inputs={}, timeout_milliseconds=5000)
        request_assist(target_name, sample, destination_mode, ui_flight_context)
        restored_flight = restore_forward_view(target_name, sample)
        last_flight_status = restored_flight["state"]
        last_prompt_text = restored_flight["text"]
        action.clear_on_failure()
        action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    else:
        emit_update("WAITING_FOR_ASSIST", sample, target_name, commanded_throttle=0, reason="ASSIST_REQUEST_CONFIRMED_AT_RESUME")

    assist_active_confirmations = 0
    alignment_required_samples = 0
    line_of_sight_required_samples = 1 if last_flight_status == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED" else 0
    line_of_sight_recovery_count = 0
    last_line_of_sight_control = None
    no_ownership_evidence_samples = 0
    waiting_at_zero_for_restored_gate = line_of_sight_required_samples == 1
    blue_zone_requested = False
    if not waiting_at_zero_for_restored_gate:
        throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
        blue_zone_requested = True
        emit_update("WAITING_FOR_ASSIST", sample, target_name, commanded_throttle=75, last_command="SET_THROTTLE_75", reason="SUPERCRUISE_ASSIST_BLUE_ZONE_REQUESTED:" + throttle["control"])
    else:
        task.sleep(milliseconds=POLL_MS)
    for _ in range(ASSIST_START_LIMIT):
        sample += 1
        guard_near_orbit(target_name, sample, "WAITING_FOR_ASSIST")
        flight = observe_flight()
        last_flight_status = flight["state"]
        last_prompt_text = flight["text"]
        target = None
        command = None
        commanded_throttle = 75 if blue_zone_requested else 0
        phase = "WAITING_FOR_ASSIST"
        if waiting_at_zero_for_restored_gate and last_flight_status != "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
            throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
            blue_zone_requested = True
            commanded_throttle = 75
            command = "SET_THROTTLE_75"
            waiting_at_zero_for_restored_gate = False
        if last_flight_status == "SUPERCRUISE_ASSIST_ACTIVE":
            no_ownership_evidence_samples = 0
            assist_active_confirmations += 1
            alignment_required_samples = 0
            line_of_sight_required_samples = 0
            phase = "ASSIST_ACTIVE"
        elif last_flight_status == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
            assist_active_confirmations = 0
            alignment_required_samples = 0
            line_of_sight_required_samples += 1
            phase = "CONFIRMING_LINE_OF_SIGHT"
            if line_of_sight_required_samples >= LINE_OF_SIGHT_GATE_CONFIRMATIONS:
                no_ownership_evidence_samples = 0
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                emit_update("CLEARING_LINE_OF_SIGHT", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, line_of_sight_required_confirmations=line_of_sight_required_samples, line_of_sight_recovery_count=line_of_sight_recovery_count, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="ASSIST_LINE_OF_SIGHT_REQUIRES_MINIMUM_THROTTLE:" + throttle["control"])
                recovered = resolve_assist_line_of_sight_prompt(target_name, sample, line_of_sight_recovery_count)
                sample = recovered["sample"]
                last_flight_status = recovered["flight"]["state"]
                last_prompt_text = recovered["flight"]["text"]
                assist_active_confirmations = recovered["assistActiveConfirmations"]
                line_of_sight_recovery_count = recovered["recoveryCount"]
                last_line_of_sight_control = recovered["control"]
                if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
                    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
                    blue_zone_requested = True
                    waiting_at_zero_for_restored_gate = False
                    command = "CLEAR_LINE_OF_SIGHT+REALIGN+SET_THROTTLE_75"
                    commanded_throttle = 75
                else:
                    command = "CLEAR_LINE_OF_SIGHT+REALIGN"
                    commanded_throttle = 0
                line_of_sight_required_samples = 0
                phase = "ASSIST_ACTIVE" if assist_active_confirmations >= ASSIST_ACTIVE_CONFIRMATIONS else "WAITING_FOR_ASSIST"
        elif last_flight_status == "FSD_ALIGNMENT_REQUIRED":
            assist_active_confirmations = 0
            alignment_required_samples += 1
            line_of_sight_required_samples = 0
            if alignment_required_samples > BLUE_ZONE_GRACE_SAMPLES:
                no_ownership_evidence_samples = 0
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                emit_update("CORRECTING_ASSIST_ALIGNMENT", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="ASSIST_ALIGNMENT_REQUIRES_MINIMUM_THROTTLE:" + throttle["control"])
                alignment = resolve_assist_alignment_prompt(target_name, sample)
                sample = alignment["sample"]
                last_flight_status = alignment["flight"]["state"]
                last_prompt_text = alignment["flight"]["text"]
                assist_active_confirmations = alignment["assistActiveConfirmations"]
                if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
                    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
                    blue_zone_requested = True
                    waiting_at_zero_for_restored_gate = False
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
            line_of_sight_required_samples = 0
            no_ownership_evidence_samples += 1
            if no_ownership_evidence_samples >= ASSIST_START_NO_EVIDENCE_LIMIT:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                emit_update(
                    "ASSIST_OWNERSHIP_TIMEOUT",
                    sample,
                    target_name,
                    flight_status=last_flight_status,
                    prompt_text=last_prompt_text,
                    assist_missing_samples=no_ownership_evidence_samples,
                    commanded_throttle=0,
                    last_command="SET_THROTTLE_0",
                    reason="NO_ASSIST_GATE_" + str(no_ownership_evidence_samples) + "_OF_" + str(ASSIST_START_NO_EVIDENCE_LIMIT) + ":" + throttle["control"],
                )
                fail("SUPERCRUISE ASSIST ownership produced no ACTIVE, alignment, or line-of-sight Gate within the bounded blue-zone window")
        else:
            fail("unexpected known flight status while awaiting Supercruise Assist activation: " + last_flight_status)
        emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, target=target, assist_active_confirmations=assist_active_confirmations, assist_missing_samples=no_ownership_evidence_samples, line_of_sight_required_confirmations=line_of_sight_required_samples, line_of_sight_recovery_count=line_of_sight_recovery_count, line_of_sight_control=last_line_of_sight_control, commanded_throttle=commanded_throttle, last_command=command, reason="WAITING_FOR_ASSIST_GATES_THEN_BLUE_ZONE_OWNERSHIP")
        if assist_active_confirmations >= ASSIST_ACTIVE_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
        fail("SUPERCRUISE ASSIST ACTIVE was not confirmed twice; Auto Throttle or Assist activation may not be active")

    stream.activity(message="Supercruise Assist owns flight controls", level="info")
    assist_missing_samples = 0
    stopped_confirmations = 0
    final_speed = None
    wgc_capture_errors = 0
    line_of_sight_required_samples = 0
    reacquiring_assist = False
    assist_reacquire_samples = 0
    agent_flight_input_after_assist_active = False
    for _ in range(ASSIST_ACTIVE_LIMIT):
        sample += 1
        near_orbit = recover_near_orbit_or_handoff(target_name, sample)
        if near_orbit["recovered"]:
            sample = near_orbit["sample"]
            line_of_sight_recovery_count += 1
            last_line_of_sight_control = near_orbit["control"]
            assist_active_confirmations = near_orbit["assistActiveConfirmations"]
            line_of_sight_required_samples = 0
            assist_missing_samples = 0
            stopped_confirmations = 0
            assist_reacquire_samples = 0
            reacquiring_assist = assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS
            agent_flight_input_after_assist_active = True
        flight = observe_flight()
        speed_attempt = action.try_call(id="elite-dangerous/ship-speed", inputs={})
        if not speed_attempt["ok"]:
            text = speed_attempt["error"]
            if transient_wgc_region_capture_error(text):
                wgc_capture_errors += 1
                emit_update("GAME_CONTROLLED_APPROACH", sample, target_name, flight_status=flight["state"], prompt_text=flight["text"], assist_active_confirmations=assist_active_confirmations, assist_missing_samples=assist_missing_samples, stopped_confirmations=stopped_confirmations, commanded_throttle=None, reason="SHIP_SPEED_WGC_CAPTURE_RETRY_" + str(wgc_capture_errors) + "_OF_" + str(MAX_WGC_CAPTURE_ERRORS))
                if wgc_capture_errors > MAX_WGC_CAPTURE_ERRORS:
                    fail("ship-speed WGC region capture error limit exceeded after five skipped errors: " + text)
                task.sleep(milliseconds=POLL_MS)
                continue
            fail("ship-speed observation failed while Supercruise Assist owns flight: " + text)
        wgc_capture_errors = 0
        speed = speed_attempt["output"]["speed"]
        final_speed = speed
        last_flight_status = flight["state"]
        last_prompt_text = flight["text"]
        if last_flight_status == "SUPERCRUISE_ASSIST_ACTIVE":
            assist_missing_samples = 0
            line_of_sight_required_samples = 0
            assist_reacquire_samples = 0
            if reacquiring_assist:
                assist_active_confirmations += 1
                if assist_active_confirmations >= ASSIST_ACTIVE_CONFIRMATIONS:
                    reacquiring_assist = False
                    stream.activity(message="Supercruise Assist ownership reacquired", level="info")
        elif last_flight_status == "SAFE_DISENGAGE_READY":
            line_of_sight_required_samples = 0
            if reacquiring_assist:
                assist_active_confirmations = 0
                assist_reacquire_samples += 1
            else:
                assist_missing_samples = 0
        elif last_flight_status == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
            assist_missing_samples = 0
            stopped_confirmations = 0
            assist_active_confirmations = 0
            line_of_sight_required_samples += 1
            if line_of_sight_required_samples >= LINE_OF_SIGHT_GATE_CONFIRMATIONS:
                if line_of_sight_recovery_count >= MAX_LINE_OF_SIGHT_RECOVERIES:
                    fail("LINE_OF_SIGHT_RECOVERY_LIMIT: Supercruise Assist obstruction recurred more than three times")
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                agent_flight_input_after_assist_active = True
                emit_update("CLEARING_LINE_OF_SIGHT", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, line_of_sight_required_confirmations=line_of_sight_required_samples, line_of_sight_recovery_count=line_of_sight_recovery_count, line_of_sight_control=last_line_of_sight_control, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="POST_OWNERSHIP_LINE_OF_SIGHT_GATE_CONFIRMED:" + throttle["control"])
                recovered = resolve_assist_line_of_sight_prompt(target_name, sample, line_of_sight_recovery_count)
                sample = recovered["sample"]
                last_flight_status = recovered["flight"]["state"]
                last_prompt_text = recovered["flight"]["text"]
                assist_active_confirmations = recovered["assistActiveConfirmations"]
                line_of_sight_recovery_count = recovered["recoveryCount"]
                last_line_of_sight_control = recovered["control"]
                line_of_sight_required_samples = 0
                assist_reacquire_samples = 0
                if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
                    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
                    reacquiring_assist = True
                    emit_update("REACQUIRING_ASSIST", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, line_of_sight_recovery_count=line_of_sight_recovery_count, line_of_sight_control=last_line_of_sight_control, commanded_throttle=75, last_command="SET_THROTTLE_75", reason="POST_SEPARATION_ALIGNMENT_CLEAR_RESTORING_BLUE_ZONE:" + throttle["control"])
                else:
                    reacquiring_assist = False
        elif last_flight_status == "FSD_ALIGNMENT_REQUIRED" and reacquiring_assist:
            throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            emit_update("CORRECTING_ASSIST_ALIGNMENT", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=0, line_of_sight_recovery_count=line_of_sight_recovery_count, line_of_sight_control=last_line_of_sight_control, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="POST_SEPARATION_ASSIST_ALIGNMENT_REQUIRED:" + throttle["control"])
            alignment = resolve_assist_alignment_prompt(target_name, sample)
            sample = alignment["sample"]
            last_flight_status = alignment["flight"]["state"]
            last_prompt_text = alignment["flight"]["text"]
            assist_active_confirmations = alignment["assistActiveConfirmations"]
            if assist_active_confirmations < ASSIST_ACTIVE_CONFIRMATIONS:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
                emit_update("REACQUIRING_ASSIST", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, line_of_sight_recovery_count=line_of_sight_recovery_count, line_of_sight_control=last_line_of_sight_control, commanded_throttle=75, last_command="SET_THROTTLE_75", reason="POST_SEPARATION_ALIGNMENT_PROMPT_CLEAR_RESTORING_BLUE_ZONE:" + throttle["control"])
            else:
                reacquiring_assist = False
        elif last_flight_status in ["UNKNOWN", "SUPERCRUISE"]:
            line_of_sight_required_samples = 0
            if reacquiring_assist:
                assist_active_confirmations = 0
                assist_missing_samples = 0
                assist_reacquire_samples += 1
            else:
                assist_missing_samples += 1
        else:
            fail("unexpected known flight status while Supercruise Assist owns flight: " + last_flight_status)
        if reacquiring_assist and assist_reacquire_samples >= ASSIST_START_LIMIT:
            fail("SUPERCRUISE ASSIST ACTIVE was not reacquired after clearing line of sight")
        if not reacquiring_assist and line_of_sight_required_samples == 0 and speed["state"] == "STOPPED":
            stopped_confirmations += 1
        else:
            stopped_confirmations = 0
        phase = "REACQUIRING_ASSIST" if reacquiring_assist else ("CONFIRMING_LINE_OF_SIGHT" if line_of_sight_required_samples > 0 else ("VERIFYING_STOP" if assist_missing_samples > 0 else "GAME_CONTROLLED_APPROACH"))
        reason = "POST_SEPARATION_WAITING_FOR_ASSIST_OWNERSHIP" if reacquiring_assist else ("LINE_OF_SIGHT_GATE_PENDING_DEBOUNCE" if line_of_sight_required_samples > 0 else "GAME_OWNS_FLIGHT_UNLESS_EXPLICIT_LINE_OF_SIGHT_RECOVERY_IS_ACTIVE")
        emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, assist_active_confirmations=assist_active_confirmations, assist_missing_samples=assist_missing_samples, line_of_sight_required_confirmations=line_of_sight_required_samples, line_of_sight_recovery_count=line_of_sight_recovery_count, line_of_sight_control=last_line_of_sight_control, stopped_confirmations=stopped_confirmations, commanded_throttle=None, reason=reason)
        if not reacquiring_assist and line_of_sight_required_samples == 0 and assist_missing_samples >= STOPPED_CONFIRMATIONS and stopped_confirmations >= STOPPED_CONFIRMATIONS:
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
                "lineOfSightRecoveryCount": line_of_sight_recovery_count,
                "stoppedConfirmations": stopped_confirmations,
                "agentFlightInputAfterAssistActive": agent_flight_input_after_assist_active,
                "finalSpeed": final_speed,
                "sampleCount": sample,
            }
        if not reacquiring_assist and line_of_sight_required_samples == 0 and assist_missing_samples >= ASSIST_MISSING_LIMIT:
            fail("ASSIST_INTERRUPTED: Supercruise Assist prompt disappeared without a visually confirmed normal-space stop")
        task.sleep(milliseconds=POLL_MS)
    fail("Supercruise Assist did not reach automatic drop and stop before the active limit")
