UI_SETTLE_MS = 1000
OBSERVATION_SETTLE_MS = 250
STABLE_ATTEMPTS = 4
POST_SELECT_ATTEMPTS = 4
MAX_SELECT_ATTEMPTS = 2
MAX_PANEL_CYCLES = 3
MAX_NAVIGATION = 8
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
MIN_TEXT_SIMILARITY = 0.75
MIN_TEXT_MARGIN = 0.15
FOCUSED_FILL_MINIMUM = 0.025
LIST_MIN_Y = 320.0
LIST_MAX_Y = 760.0
LIST_MAX_X = 650.0

def emit_update(phase, station_name, target_state, observation, command, panel_cycles, navigation_count, opened_panel):
    stream.emit(
        type="action.select-station-target.update",
        payload={
            "phase": phase,
            "stationName": station_name,
            "targetState": target_state,
            "observation": observation,
            "command": command,
            "panelCycleCount": panel_cycles,
            "navigationCount": navigation_count,
            "openedPanel": opened_panel,
        },
    )

def normalize_text(text):
    normalized = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
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
    return {
        "left": minimum_x,
        "right": maximum_x,
        "top": minimum_y,
        "bottom": maximum_y,
        "centerY": (minimum_y + maximum_y) / 2.0,
    }

def is_list_region(region):
    if len(region["referencePoints"]) != 4:
        return False
    box = bounds(region)
    return box["left"] < LIST_MAX_X and box["centerY"] >= LIST_MIN_Y and box["centerY"] <= LIST_MAX_Y

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
        pixel = pixels[index]
        inspected += 1
        red, green, blue = pixel_channels(pixel)
        if red >= 60 and green >= 12 and blue <= 40 and red >= green + 10:
            focused += 1
    return float(focused) / float(inspected)

def inspect_target(raw, station_name):
    expected = normalize_text(station_name)
    if len(expected) < 2:
        fail("stationName must contain at least two letters or digits")
    best = None
    runner_up_score = 0.0
    focused_rows = []
    meaningful = 0
    for region in raw["regions"]:
        if not is_list_region(region):
            continue
        normalized = normalize_text(region["text"])
        if region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and len(normalized) > 0:
            meaningful += 1
            text_similarity = similarity(normalized, expected)
            score = region["recognitionConfidence"] * text_similarity
            candidate = {
                "region": region,
                "normalizedText": normalized,
                "similarity": text_similarity,
                "score": score,
                "bounds": bounds(region),
            }
            if best == None or score > best["score"]:
                if best != None and best["score"] > runner_up_score:
                    runner_up_score = best["score"]
                best = candidate
            elif score > runner_up_score:
                runner_up_score = score
            ratio = focus_fill_ratio(region)
            if ratio != None and ratio >= FOCUSED_FILL_MINIMUM:
                focused_rows.append({"text": region["text"], "centerY": bounds(region)["centerY"], "fillRatio": ratio})
    margin = 0.0 if best == None else best["score"] - runner_up_score
    accepted = best != None and best["region"]["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and best["region"]["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and best["similarity"] >= MIN_TEXT_SIMILARITY and margin >= MIN_TEXT_MARGIN
    if not accepted:
        return {
            "state": "UNKNOWN",
            "locked": None,
            "focused": None,
            "direction": None,
            "text": None if best == None else best["region"]["text"],
            "similarity": 0.0 if best == None else best["similarity"],
            "margin": margin,
            "meaningfulRegionCount": meaningful,
            "focusFillRatio": None,
            "reason": "TARGET_TEXT_NOT_CONFIRMED",
        }
    text = best["region"]["text"]
    locked = "<" in text and ">" in text
    target_fill = focus_fill_ratio(best["region"])
    focused = target_fill != None and target_fill >= FOCUSED_FILL_MINIMUM
    state = "LOCKED" if locked else ("FOCUSED" if focused else "VISIBLE")
    direction = None
    reason = "ANGLE_BRACKETS_CONFIRMED" if locked else ("TARGET_ROW_FOCUSED" if focused else "TARGET_ROW_VISIBLE")
    if not locked and not focused:
        if len(focused_rows) != 1:
            state = "UNKNOWN"
            reason = "FOCUSED_ROW_NOT_UNIQUE"
        elif focused_rows[0]["centerY"] < best["bounds"]["centerY"]:
            direction = "DOWN"
        elif focused_rows[0]["centerY"] > best["bounds"]["centerY"]:
            direction = "UP"
        else:
            state = "UNKNOWN"
            reason = "FOCUS_GEOMETRY_AMBIGUOUS"
    return {
        "state": state,
        "locked": locked,
        "focused": focused,
        "direction": direction,
        "text": text,
        "similarity": best["similarity"],
        "margin": margin,
        "meaningfulRegionCount": meaningful,
        "focusFillRatio": target_fill,
        "reason": reason,
    }

def observe_target(station_name):
    raw = action.call(id="elite-dangerous/station-contact-text-regions", inputs={})
    return inspect_target(raw, station_name)

def observe_target_stable(station_name, phase, panel_cycles, navigation_count, opened_panel):
    previous = None
    count = 0
    for attempt in range(STABLE_ATTEMPTS):
        observation = observe_target(station_name)
        count += 1
        emit_update(phase, station_name, observation["state"], observation, None, panel_cycles, navigation_count, opened_panel)
        key = observation["state"] + ":" + str(observation["text"])
        if observation["state"] != "UNKNOWN" and key == previous:
            return {"observation": observation, "count": count}
        previous = None if observation["state"] == "UNKNOWN" else key
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Station target did not produce two consecutive known observations")

def verify_target_lock(station_name, panel_cycles, navigation_count, opened_panel):
    observation_count = 0
    final = None
    for select_attempt in range(MAX_SELECT_ATTEMPTS):
        previous_locked = None
        focused_count = 0
        for verify_attempt in range(POST_SELECT_ATTEMPTS):
            observation = observe_target(station_name)
            observation_count += 1
            final = observation
            emit_update("VERIFYING_LOCK", station_name, observation["state"], observation, None, panel_cycles, navigation_count, opened_panel)
            key = observation["state"] + ":" + str(observation["text"])
            if observation["state"] == "LOCKED":
                focused_count = 0
                if key == previous_locked:
                    return {"observation": observation, "count": observation_count, "selectAttemptCount": select_attempt + 1}
                previous_locked = key
            else:
                previous_locked = None
                if observation["state"] == "FOCUSED":
                    focused_count += 1
                else:
                    focused_count = 0
            if verify_attempt + 1 < POST_SELECT_ATTEMPTS:
                task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
        if select_attempt + 1 >= MAX_SELECT_ATTEMPTS:
            break
        if final["state"] != "FOCUSED" or focused_count < 2:
            fail("Station target transition remained ambiguous after SELECT: " + str(final))
        stream.activity(message="Station target SELECT was not accepted; retrying once", level="warning")
        emit_update("SELECTING_TARGET", station_name, final["state"], final, "SELECT", panel_cycles, navigation_count, opened_panel)
        action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
        task.sleep(milliseconds=UI_SETTLE_MS)
    fail("two bounded SELECT attempts were not followed by two angle-bracketed Station target observations: " + str(final))

def observe_contacts_stable():
    previous = None
    count = 0
    for attempt in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/contacts-tab-state", inputs={})
        count += 1
        state = observation["activeTab"]["state"]
        if state != "UNKNOWN" and state == previous:
            return {"observation": observation, "count": count}
        previous = None if state == "UNKNOWN" else state
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Contacts tab did not produce two consecutive known observations")

def restore_owned_panel(station_name, state, observation, panel_cycles, navigation_count, opened_panel):
    if not opened_panel:
        return False
    emit_update("RESTORING_VIEW", station_name, state, observation, "FOCUS_LEFT_PANEL", panel_cycles, navigation_count, opened_panel)
    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    task.sleep(milliseconds=UI_SETTLE_MS)
    stable = observe_contacts_stable()
    if stable["observation"]["activeTab"]["state"] != "ABSENT":
        fail("left panel remained visible after restoring the owned view")
    action.clear_on_failure()
    return True

def main(ctx):
    station_name = ctx.inputs["stationName"]
    opened_panel = False
    panel_cycles = 0
    navigation_count = 0
    observation_count = 0
    select_sent = False
    select_attempt_count = 0

    contacts_stable = observe_contacts_stable()
    observation_count += contacts_stable["count"]
    contacts = contacts_stable["observation"]
    state = contacts["activeTab"]["state"]
    emit_update("OPENING_CONTACTS", station_name, None, contacts, None, panel_cycles, navigation_count, opened_panel)
    if state == "ABSENT":
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        opened_panel = True
        action.on_failure(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        emit_update("OPENING_CONTACTS", station_name, None, contacts, "FOCUS_LEFT_PANEL", panel_cycles, navigation_count, opened_panel)
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts_stable = observe_contacts_stable()
        observation_count += contacts_stable["count"]
        contacts = contacts_stable["observation"]
        state = contacts["activeTab"]["state"]
        if state == "ABSENT":
            fail("left panel remained absent after FOCUS_LEFT_PANEL")
    for _ in range(MAX_PANEL_CYCLES):
        if state == "CONTACTS":
            break
        action.call(id="elite-dangerous/ui-control", inputs={"control": "NEXT_PANEL"})
        panel_cycles += 1
        emit_update("OPENING_CONTACTS", station_name, None, contacts, "NEXT_PANEL", panel_cycles, navigation_count, opened_panel)
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts_stable = observe_contacts_stable()
        observation_count += contacts_stable["count"]
        contacts = contacts_stable["observation"]
        state = contacts["activeTab"]["state"]
        if state == "ABSENT":
            fail("Contacts scan region became absent after NEXT_PANEL")
    if state != "CONTACTS":
        fail("CONTACTS was not reached within three NEXT_PANEL inputs")

    final_observation = None
    result = None
    for _ in range(MAX_NAVIGATION + 1):
        stable = observe_target_stable(station_name, "LOCATING_TARGET", panel_cycles, navigation_count, opened_panel)
        observation_count += stable["count"]
        target = stable["observation"]
        final_observation = target
        if target["state"] == "LOCKED":
            result = "EXISTING" if not select_sent else "ACQUIRED"
            break
        if target["state"] == "FOCUSED":
            emit_update("SELECTING_TARGET", station_name, target["state"], target, "SELECT", panel_cycles, navigation_count, opened_panel)
            action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
            select_sent = True
            task.sleep(milliseconds=UI_SETTLE_MS)
            verified = verify_target_lock(station_name, panel_cycles, navigation_count, opened_panel)
            observation_count += verified["count"]
            select_attempt_count = verified["selectAttemptCount"]
            final_observation = verified["observation"]
            result = "ACQUIRED"
            break
        if target["state"] != "VISIBLE" or target["direction"] == None:
            fail("named Station target or its unique focused row was not visually confirmed")
        if navigation_count >= MAX_NAVIGATION:
            fail("named Station target was not focused within eight directional inputs")
        command = target["direction"]
        emit_update("MOVING_FOCUS", station_name, target["state"], target, command, panel_cycles, navigation_count, opened_panel)
        action.call(id="elite-dangerous/ui-control", inputs={"control": command})
        navigation_count += 1
        task.sleep(milliseconds=UI_SETTLE_MS)

    if result == None:
        fail("named Station target lock was not established")
    restored_view = restore_owned_panel(station_name, final_observation["state"], final_observation, panel_cycles, navigation_count, opened_panel)
    stream.activity(message="Station target lock confirmed: " + station_name, level="info")
    emit_update("COMPLETED", station_name, "LOCKED", final_observation, None, panel_cycles, navigation_count, opened_panel)
    return {
        "schemaVersion": 1,
        "task": "SELECT_STATION_TARGET",
        "completed": True,
        "result": result,
        "stationName": station_name,
        "targetLocked": True,
        "selectSent": select_sent,
        "selectAttemptCount": select_attempt_count,
        "openedPanel": opened_panel,
        "restoredView": restored_view,
        "panelCycleCount": panel_cycles,
        "navigationCount": navigation_count,
        "observationCount": observation_count,
        "finalObservation": final_observation,
    }
