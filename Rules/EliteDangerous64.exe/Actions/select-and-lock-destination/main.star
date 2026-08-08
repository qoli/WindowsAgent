UI_SETTLE_MS = 1000
OBSERVATION_SETTLE_MS = 250
STABLE_ATTEMPTS = 4
MAX_PANEL_CYCLES = 3
MAX_NAVIGATION = 8
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
MIN_TEXT_SIMILARITY = 0.75
MIN_TEXT_MARGIN = 0.15
FOCUSED_FILL_MINIMUM = 0.50
LIST_MIN_Y = 320.0
LIST_MAX_Y = 760.0
LIST_MAX_X = 920.0

def emit_update(phase, target_name, target_state, observation, command, panel_cycles, navigation_count, opened_panel):
    stream.emit(
        type="action.select-and-lock-destination.update",
        payload={
            "phase": phase,
            "targetName": target_name,
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

def inspect_target(raw, target_name):
    expected = normalize_text(target_name)
    best = None
    runner_up_score = 0.0
    focused_rows = []
    meaningful = 0
    for region in raw["regions"]:
        if len(region["referencePoints"]) != 4:
            continue
        box = bounds(region)
        if box["left"] >= LIST_MAX_X or box["centerY"] < LIST_MIN_Y or box["centerY"] > LIST_MAX_Y:
            continue
        normalized = normalize_text(region["text"])
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE or len(normalized) == 0:
            continue
        meaningful += 1
        text_similarity = similarity(normalized, expected)
        score = region["recognitionConfidence"] * text_similarity
        candidate = {"region": region, "similarity": text_similarity, "score": score, "bounds": box}
        if best == None or score > best["score"]:
            if best != None and best["score"] > runner_up_score:
                runner_up_score = best["score"]
            best = candidate
        elif score > runner_up_score:
            runner_up_score = score
        ratio = focus_fill_ratio(region)
        if ratio != None and ratio >= FOCUSED_FILL_MINIMUM:
            focused_rows.append({"text": region["text"], "centerY": box["centerY"], "fillRatio": ratio})
    margin = 0.0 if best == None else best["score"] - runner_up_score
    if best == None or best["similarity"] < MIN_TEXT_SIMILARITY or margin < MIN_TEXT_MARGIN:
        return {"state": "UNKNOWN", "locked": None, "focused": None, "direction": None, "text": None if best == None else best["region"]["text"], "similarity": 0.0 if best == None else best["similarity"], "margin": margin, "meaningfulRegionCount": meaningful, "focusFillRatio": None, "reason": "TARGET_TEXT_NOT_CONFIRMED"}
    text = best["region"]["text"]
    locked = "<" in text and ">" in text
    target_fill = focus_fill_ratio(best["region"])
    focused = target_fill != None and target_fill >= FOCUSED_FILL_MINIMUM
    # These rows belong specifically to NAVIGATION: angle brackets are direct
    # LOCK DESTINATION evidence here. CONTACTS uses the same glyphs for a
    # different ship-target concept and is classified by another Action.
    state = "LOCKED" if locked else ("FOCUSED" if focused else "VISIBLE")
    direction = None
    reason = "NAVIGATION_DESTINATION_BRACKETS_CONFIRMED" if locked else ("TARGET_ROW_FOCUSED" if focused else "TARGET_ROW_VISIBLE")
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
    return {"state": state, "locked": locked, "focused": focused, "direction": direction, "text": text, "similarity": best["similarity"], "margin": margin, "meaningfulRegionCount": meaningful, "focusFillRatio": target_fill, "reason": reason}

def observe_target_stable(target_name, phase, panel_cycles, navigation_count, opened_panel):
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        raw = action.call(id="elite-dangerous/navigation-list-text-regions", inputs={})
        observation = inspect_target(raw, target_name)
        emit_update(phase, target_name, observation["state"], observation, None, panel_cycles, navigation_count, opened_panel)
        key = observation["state"] + ":" + str(observation["text"])
        if observation["state"] != "UNKNOWN" and key == previous:
            return {"observation": observation, "count": attempt + 1}
        previous = None if observation["state"] == "UNKNOWN" else key
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("named Navigation target did not produce two consecutive known observations")

def observe_contacts_stable():
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        observation = action.call(id="elite-dangerous/contacts-tab-state", inputs={})
        state = observation["activeTab"]["state"]
        if state != "UNKNOWN" and state == previous:
            return {"observation": observation, "count": attempt + 1}
        previous = None if state == "UNKNOWN" else state
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Contacts tab did not produce two consecutive known observations")

def inspect_action_label(raw):
    best = None
    for region in raw["regions"]:
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        normalized = normalize_text(region["text"])
        lock_similarity = similarity(normalized, "LOCKDESTINATION")
        unlock_similarity = similarity(normalized, "UNLOCKDESTINATION")
        state = "LOCK" if lock_similarity >= unlock_similarity else "UNLOCK"
        score = lock_similarity if state == "LOCK" else unlock_similarity
        candidate = {"state": state if score >= MIN_TEXT_SIMILARITY else "UNKNOWN", "text": region["text"], "similarity": score, "recognitionConfidence": region["recognitionConfidence"], "referencePoints": region["referencePoints"]}
        if best == None or candidate["similarity"] * candidate["recognitionConfidence"] > best["similarity"] * best["recognitionConfidence"]:
            best = candidate
    if best == None:
        return {"state": "UNKNOWN", "text": None, "similarity": 0.0, "recognitionConfidence": None, "referencePoints": None}
    return best

def observe_detail_stable(target_name, panel_cycles, navigation_count, opened_panel):
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        button = action.call(id="elite-dangerous/lock-destination-button-state", inputs={})["button"]
        raw = action.call(id="elite-dangerous/lock-destination-text-regions", inputs={})
        label = inspect_action_label(raw)
        observation = {"button": button, "label": label, "timing": raw["timing"]}
        emit_update("VERIFYING_DETAIL", target_name, label["state"], observation, None, panel_cycles, navigation_count, opened_panel)
        key = label["state"] + ":" + button["state"]
        if label["state"] != "UNKNOWN" and button["state"] == "FOCUSED" and key == previous:
            return {"observation": observation, "count": attempt + 1}
        previous = key if label["state"] != "UNKNOWN" and button["state"] == "FOCUSED" else None
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Navigation detail did not produce two consecutive focused LOCK or UNLOCK observations")

def restore_owned_panel(target_name, observation, panel_cycles, navigation_count, opened_panel):
    if not opened_panel:
        return False
    emit_update("RESTORING_VIEW", target_name, None, observation, "FOCUS_LEFT_PANEL", panel_cycles, navigation_count, opened_panel)
    action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
    task.sleep(milliseconds=UI_SETTLE_MS)
    stable = observe_contacts_stable()
    if stable["observation"]["activeTab"]["state"] != "ABSENT":
        fail("left panel remained visible after restoring the owned view")
    action.clear_on_failure()
    return True

def main(ctx):
    target_name = ctx.inputs["targetName"]
    if len(normalize_text(target_name)) < 2:
        fail("targetName must contain at least two letters or digits")
    opened_panel = False
    panel_cycles = 0
    navigation_count = 0
    observation_count = 0
    row_select_sent = False
    lock_select_sent = False

    contacts_stable = observe_contacts_stable()
    observation_count += contacts_stable["count"]
    contacts = contacts_stable["observation"]
    state = contacts["activeTab"]["state"]
    emit_update("OPENING_PANEL", target_name, None, contacts, None, panel_cycles, navigation_count, opened_panel)
    if state == "ABSENT":
        action.call(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        opened_panel = True
        action.on_failure(id="elite-dangerous/ui-control", inputs={"control": "FOCUS_LEFT_PANEL"})
        emit_update("OPENING_PANEL", target_name, None, contacts, "FOCUS_LEFT_PANEL", panel_cycles, navigation_count, opened_panel)
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts_stable = observe_contacts_stable()
        observation_count += contacts_stable["count"]
        contacts = contacts_stable["observation"]
        state = contacts["activeTab"]["state"]
        if state == "ABSENT":
            fail("left panel remained absent after FOCUS_LEFT_PANEL")
    for _ in range(MAX_PANEL_CYCLES):
        if state == "NAVIGATION":
            break
        action.call(id="elite-dangerous/ui-control", inputs={"control": "NEXT_PANEL"})
        panel_cycles += 1
        emit_update("OPENING_PANEL", target_name, None, contacts, "NEXT_PANEL", panel_cycles, navigation_count, opened_panel)
        task.sleep(milliseconds=UI_SETTLE_MS)
        contacts_stable = observe_contacts_stable()
        observation_count += contacts_stable["count"]
        contacts = contacts_stable["observation"]
        state = contacts["activeTab"]["state"]
        if state == "ABSENT":
            fail("Left panel header became absent while selecting NAVIGATION")
    if state != "NAVIGATION":
        fail("NAVIGATION was not reached within three NEXT_PANEL inputs")

    final_observation = None
    result = None
    for _ in range(MAX_NAVIGATION + 1):
        stable = observe_target_stable(target_name, "LOCATING_TARGET", panel_cycles, navigation_count, opened_panel)
        observation_count += stable["count"]
        target = stable["observation"]
        final_observation = target
        if target["state"] == "LOCKED":
            result = "EXISTING"
            break
        if target["state"] == "FOCUSED":
            emit_update("OPENING_DETAIL", target_name, target["state"], target, "SELECT", panel_cycles, navigation_count, opened_panel)
            action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
            row_select_sent = True
            task.sleep(milliseconds=UI_SETTLE_MS)
            detail = observe_detail_stable(target_name, panel_cycles, navigation_count, opened_panel)
            observation_count += detail["count"]
            label_state = detail["observation"]["label"]["state"]
            if label_state == "UNLOCK":
                final_observation = detail["observation"]
                result = "EXISTING"
                break
            if label_state != "LOCK":
                fail("focused Navigation detail action is not LOCK DESTINATION")
            emit_update("SELECTING_LOCK", target_name, label_state, detail["observation"], "SELECT", panel_cycles, navigation_count, opened_panel)
            action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
            lock_select_sent = True
            task.sleep(milliseconds=UI_SETTLE_MS)
            verified = observe_target_stable(target_name, "VERIFYING_LOCK", panel_cycles, navigation_count, opened_panel)
            observation_count += verified["count"]
            final_observation = verified["observation"]
            if final_observation["state"] != "LOCKED":
                fail("LOCK DESTINATION was not followed by two angle-bracketed target observations")
            result = "ACQUIRED"
            break
        if target["state"] != "VISIBLE" or target["direction"] == None:
            fail("named Navigation target or its unique focused row was not visually confirmed")
        if navigation_count >= MAX_NAVIGATION:
            fail("named Navigation target was not focused within eight directional inputs")
        command = target["direction"]
        emit_update("MOVING_FOCUS", target_name, target["state"], target, command, panel_cycles, navigation_count, opened_panel)
        action.call(id="elite-dangerous/ui-control", inputs={"control": command})
        navigation_count += 1
        task.sleep(milliseconds=UI_SETTLE_MS)

    if result == None:
        fail("named Navigation destination lock was not established")
    restored_view = restore_owned_panel(target_name, final_observation, panel_cycles, navigation_count, opened_panel)
    stream.activity(message="Navigation destination lock confirmed: " + target_name, level="info")
    emit_update("COMPLETED", target_name, "LOCKED", final_observation, None, panel_cycles, navigation_count, opened_panel)
    return {"schemaVersion": 1, "task": "SELECT_AND_LOCK_DESTINATION", "completed": True, "result": result, "targetName": target_name, "targetLocked": True, "rowSelectSent": row_select_sent, "lockSelectSent": lock_select_sent, "openedPanel": opened_panel, "restoredView": restored_view, "panelCycleCount": panel_cycles, "navigationCount": navigation_count, "observationCount": observation_count, "finalObservation": final_observation}
