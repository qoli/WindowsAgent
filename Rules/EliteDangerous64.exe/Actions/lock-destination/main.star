UI_SETTLE_MS = 1000
OBSERVATION_SETTLE_MS = 250
STABLE_ATTEMPTS = 4
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
MIN_TEXT_SIMILARITY = 0.75

def emit_update(phase, target_name, label_state, button_state, select_sent, observation, command=None, reason=None):
    stream.emit(
        type="action.lock-destination.update",
        payload={
            "phase": phase,
            "targetName": target_name,
            "labelState": label_state,
            "buttonState": button_state,
            "selectSent": select_sent,
            "observation": observation,
            "command": command,
            "reason": reason,
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
        candidate = {
            "state": state if score >= MIN_TEXT_SIMILARITY else "UNKNOWN",
            "text": region["text"],
            "normalizedText": normalized,
            "similarity": score,
            "recognitionConfidence": region["recognitionConfidence"],
            "referencePoints": region["referencePoints"],
        }
        if best == None or candidate["similarity"] * candidate["recognitionConfidence"] > best["similarity"] * best["recognitionConfidence"]:
            best = candidate
    if best == None:
        return {"state": "UNKNOWN", "text": None, "normalizedText": None, "similarity": 0.0, "recognitionConfidence": None, "referencePoints": None}
    return best

def observe_detail():
    button = action.call(id="elite-dangerous/lock-destination-button-state", inputs={})
    raw = action.call(id="elite-dangerous/lock-destination-text-regions", inputs={})
    return {"button": button["button"], "label": inspect_action_label(raw), "timing": raw["timing"]}

def observe_detail_stable(target_name, select_sent):
    previous = None
    for attempt in range(STABLE_ATTEMPTS):
        observation = observe_detail()
        label_state = observation["label"]["state"]
        button_state = observation["button"]["state"]
        emit_update("VERIFYING_DETAIL", target_name, label_state, button_state, select_sent, observation)
        key = label_state + ":" + button_state
        if label_state != "UNKNOWN" and button_state == "FOCUSED" and key == previous:
            return observation
        previous = key if label_state != "UNKNOWN" and button_state == "FOCUSED" else None
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("Lock Destination detail action did not produce two consecutive focused known observations")

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

def inspect_locked_target(raw, target_name):
    expected = normalize_text(target_name)
    best = None
    for region in raw["regions"]:
        if len(region["referencePoints"]) != 4:
            continue
        box = bounds(region)
        if box["left"] >= 700 or box["centerY"] < 320 or box["centerY"] > 760:
            continue
        normalized = normalize_text(region["text"])
        text_similarity = similarity(normalized, expected)
        score = text_similarity * region["recognitionConfidence"]
        if region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and (best == None or score > best["score"]):
            best = {"text": region["text"], "normalizedText": normalized, "similarity": text_similarity, "score": score, "referencePoints": region["referencePoints"]}
    if best == None or best["similarity"] < MIN_TEXT_SIMILARITY:
        return {"state": "UNKNOWN", "text": None if best == None else best["text"], "similarity": 0.0 if best == None else best["similarity"], "referencePoints": None if best == None else best["referencePoints"]}
    locked = "<" in best["text"] and ">" in best["text"]
    return {"state": "LOCKED" if locked else "VISIBLE", "text": best["text"], "similarity": best["similarity"], "referencePoints": best["referencePoints"]}

def verify_locked_target(target_name):
    previous = None
    final = None
    for attempt in range(STABLE_ATTEMPTS):
        raw = action.call(id="elite-dangerous/navigation-list-text-regions", inputs={})
        observation = inspect_locked_target(raw, target_name)
        final = observation
        emit_update("VERIFYING_LOCK", target_name, None, None, True, observation)
        key = observation["state"] + ":" + str(observation["text"])
        if observation["state"] == "LOCKED" and key == previous:
            return observation
        previous = key if observation["state"] == "LOCKED" else None
        if attempt + 1 < STABLE_ATTEMPTS:
            task.sleep(milliseconds=OBSERVATION_SETTLE_MS)
    fail("SELECT was not followed by two consecutive angle-bracketed destination observations: " + str(final))

def main(ctx):
    target_name = ctx.inputs["targetName"]
    if len(normalize_text(target_name)) < 2:
        fail("targetName must contain at least two letters or digits")

    detail = observe_detail_stable(target_name, False)
    label_state = detail["label"]["state"]
    if label_state == "UNLOCK":
        emit_update("COMPLETED", target_name, label_state, detail["button"]["state"], False, detail, reason="DESTINATION_ALREADY_LOCKED")
        return {
            "schemaVersion": 1,
            "task": "LOCK_DESTINATION",
            "completed": True,
            "result": "EXISTING",
            "targetName": target_name,
            "targetLocked": True,
            "selectSent": False,
            "finalObservation": detail,
        }
    if label_state != "LOCK":
        fail("Navigation detail primary action is not LOCK DESTINATION")

    emit_update("SELECTING", target_name, label_state, detail["button"]["state"], True, detail, command="SELECT")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    task.sleep(milliseconds=UI_SETTLE_MS)
    locked = verify_locked_target(target_name)
    stream.activity(message="Destination lock confirmed: " + target_name, level="info")
    emit_update("COMPLETED", target_name, "LOCKED", None, True, locked, reason="ANGLE_BRACKETS_CONFIRMED")
    return {
        "schemaVersion": 1,
        "task": "LOCK_DESTINATION",
        "completed": True,
        "result": "ACQUIRED",
        "targetName": target_name,
        "targetLocked": True,
        "selectSent": True,
        "finalObservation": locked,
    }
