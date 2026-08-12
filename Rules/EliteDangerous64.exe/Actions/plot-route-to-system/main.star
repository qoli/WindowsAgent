UI_SETTLE_MS = 1000
OCR_SETTLE_MS = 250
ROUTE_POLL_LIMIT = 20
SEARCH_CLEAR_LIMIT = 96
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
SUGGESTION_MIN_Y = 150.0
SUGGESTION_MAX_Y = 235.0

def emit_update(phase, target_system, command=None, observation=None, reason=None):
    stream.emit(
        type="action.plot-route-to-system.update",
        payload={
            "phase": phase,
            "targetSystem": target_system,
            "command": command,
            "observation": observation,
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
        "centerX": (minimum_x + maximum_x) / 2.0,
        "centerY": (minimum_y + maximum_y) / 2.0,
    }

def trusted(region):
    return region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE

def inspect_search(raw, target_system):
    expected = normalize_text(target_system)
    map_title = False
    exact = []
    for region in raw["regions"]:
        if not trusted(region):
            continue
        normalized = normalize_text(region["text"])
        box = bounds(region)
        # The orange GALAXY MAP label and white REALISTIC mode text overlap in
        # this fixed title box. PP-OCR may interleave duplicated transition
        # glyphs (for example GALAXYEMAPTPPREALISTIC). Presence remains bounded
        # to one trusted title line with the title prefix and both required
        # words; this rule is never used for target identity.
        if normalized.startswith("GALAXY") and "MAP" in normalized and "REALISTIC" in normalized:
            map_title = True
        if normalized == expected and box["centerY"] >= SUGGESTION_MIN_Y and box["centerY"] <= SUGGESTION_MAX_Y:
            exact.append({"text": region["text"], "bounds": box, "recognitionConfidence": region["recognitionConfidence"]})
    suggestion = exact[0] if len(exact) == 1 else None
    return {"mapPresent": map_title, "exactSuggestionCount": len(exact), "suggestion": suggestion}

def inspect_selected_system(raw, target_system):
    expected = normalize_text(target_system)
    exact = []
    for region in raw["regions"]:
        if trusted(region) and normalize_text(region["text"]) == expected:
            exact.append({"text": region["text"], "recognitionConfidence": region["recognitionConfidence"], "bounds": bounds(region)})
    return {"confirmed": len(exact) == 1, "exactMatchCount": len(exact), "match": exact[0] if len(exact) == 1 else None}

def observe_map_stable(target_system, expected_present, phase):
    previous = False
    last = None
    count = 0
    for _ in range(4):
        raw = action.call(id="elite-dangerous/galaxy-map-search-text-regions", inputs={})
        last = inspect_search(raw, target_system)
        count += 1
        emit_update(phase, target_system, observation=last, reason="GALAXY_MAP_TITLE")
        present = last["mapPresent"]
        if present == expected_present and previous:
            return {"observation": last, "count": count}
        previous = present == expected_present
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("Galaxy Map presence did not reach two stable observations")

def observe_initial_map_state(target_system):
    previous = None
    last = None
    count = 0
    for _ in range(4):
        raw = action.call(id="elite-dangerous/galaxy-map-search-text-regions", inputs={})
        last = inspect_search(raw, target_system)
        count += 1
        state = "PRESENT" if last["mapPresent"] else "ABSENT"
        emit_update("OPENING_MAP", target_system, observation=last, reason="GALAXY_MAP_INITIAL_" + state)
        if state == previous:
            return {"present": last["mapPresent"], "observation": last, "count": count}
        previous = state
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("initial Galaxy Map presence did not reach two stable observations")

def observe_exact_suggestion(target_system):
    previous = None
    last = None
    count = 0
    for _ in range(6):
        raw = action.call(id="elite-dangerous/galaxy-map-search-text-regions", inputs={})
        last = inspect_search(raw, target_system)
        count += 1
        suggestion = last["suggestion"]
        # OCR geometry may sub-pixel jitter between otherwise identical
        # frames. Stability belongs to the unique complete name; the latest
        # box remains the click authority after that name is stable.
        key = None if suggestion == None else normalize_text(suggestion["text"])
        emit_update("CONFIRMING_SUGGESTION", target_system, observation=last, reason="EXACT_COMPLETE_NAME_REQUIRED")
        if suggestion != None and key == previous:
            return {"observation": last, "count": count}
        previous = key
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("complete exact System suggestion did not produce two stable observations")

def observe_selected_system(target_system):
    previous = False
    last = None
    count = 0
    for _ in range(6):
        raw = action.call(id="elite-dangerous/galaxy-map-system-info-text-regions", inputs={})
        last = inspect_selected_system(raw, target_system)
        count += 1
        emit_update("CONFIRMING_SYSTEM", target_system, observation=last, reason="SYSTEM_INFORMATION_EXACT_NAME")
        if last["confirmed"] and previous:
            return {"observation": last, "count": count}
        previous = last["confirmed"]
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("selected Galaxy Map System did not produce two exact-name observations")

def route_attempt(target_system, max_jumps):
    raw_attempt = action.try_call(id="elite-dangerous/filesystem/nav-route", inputs={})
    if not raw_attempt["ok"]:
        return {"ok": False, "reason": raw_attempt["error"]}
    plan_attempt = action.try_call(id="elite-dangerous/nav-route-plan", inputs={
        "raw": raw_attempt["output"],
        "expectedDestinationSystem": target_system,
        "maxJumps": max_jumps,
    })
    if not plan_attempt["ok"]:
        return {"ok": False, "reason": plan_attempt["error"]}
    return {"ok": True, "plan": plan_attempt["output"]}

def text_key(character):
    if character == " ":
        return "SPACE"
    if character == "-":
        return "HYPHEN"
    upper = character.upper()
    if upper in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
        return upper
    fail("targetSystem contains a character unsupported by exact Galaxy Map text entry")

def completed(target_system, result, plan, exact_suggestion, selected_system, opened_map, observation_count):
    return {
        "schemaVersion": 1,
        "task": "PLOT_ROUTE_TO_SYSTEM",
        "completed": True,
        "result": result,
        "targetSystem": target_system,
        "exactSuggestionConfirmed": exact_suggestion,
        "selectedSystemConfirmed": selected_system,
        "routeId": plan["routeId"],
        "jumpCount": plan["jumpCount"],
        "routeFreshness": plan["freshness"],
        "openedMap": opened_map,
        "restoredView": True,
        "observationCount": observation_count,
    }

def main(ctx):
    target_system = ctx.inputs["targetSystem"]
    max_jumps = ctx.inputs["maxJumps"]
    if len(normalize_text(target_system)) < 2:
        fail("targetSystem must contain at least two letters or digits")
    for index in range(len(target_system)):
        text_key(target_system[index])

    emit_update("CHECKING_ROUTE", target_system, reason="NAV_ROUTE_EXACT_DESTINATION")
    existing = route_attempt(target_system, max_jumps)
    if existing["ok"]:
        stream.activity(message="Exact Galaxy Map route already plotted: " + target_system, level="info")
        emit_update("COMPLETED", target_system, observation=existing["plan"], reason="EXISTING_EXACT_ROUTE")
        return completed(target_system, "EXISTING", existing["plan"], False, False, False, 1)

    observation_count = 0
    initial = observe_initial_map_state(target_system)
    observation_count += initial["count"]
    opened_map = False
    if initial["present"]:
        action.on_failure(id="elite-dangerous/ui-control", inputs={"control": "OPEN_GALAXY_MAP"})
    else:
        emit_update("OPENING_MAP", target_system, command="OPEN_GALAXY_MAP", observation=initial["observation"], reason="MAP_ABSENT_CONFIRMED")
        action.call(id="elite-dangerous/ui-control", inputs={"control": "OPEN_GALAXY_MAP"})
        action.on_failure(id="elite-dangerous/ui-control", inputs={"control": "OPEN_GALAXY_MAP"})
        opened_map = True
        task.sleep(milliseconds=UI_SETTLE_MS)
        opened = observe_map_stable(target_system, True, "OPENING_MAP")
        observation_count += opened["count"]

    emit_update("FOCUSING_SEARCH", target_system, command="POINTER_CLICK", observation={"x": 900, "y": 135}, reason="FIXED_REFERENCE_SEARCH_FIELD")
    action.call(id="elite-dangerous/pointer-click", inputs={"x": 900, "y": 135, "holdMs": 40})
    task.sleep(milliseconds=OCR_SETTLE_MS)
    for _ in range(SEARCH_CLEAR_LIMIT):
        action.call(id="elite-dangerous/text-entry-key", inputs={"key": "BACKSPACE"})
    for index in range(len(target_system)):
        key = text_key(target_system[index])
        emit_update("ENTERING_NAME", target_system, command=key, reason="COMPLETE_EXACT_NAME")
        action.call(id="elite-dangerous/text-entry-key", inputs={"key": key})

    suggestion = observe_exact_suggestion(target_system)
    observation_count += suggestion["count"]
    box = suggestion["observation"]["suggestion"]["bounds"]
    click_x = int(box["centerX"])
    click_y = int(box["centerY"])
    emit_update("SELECTING_SUGGESTION", target_system, command="POINTER_CLICK", observation={"x": click_x, "y": click_y, "suggestion": suggestion["observation"]["suggestion"]}, reason="EXACT_SUGGESTION")
    action.call(id="elite-dangerous/pointer-click", inputs={"x": click_x, "y": click_y, "holdMs": 40})
    task.sleep(milliseconds=UI_SETTLE_MS)

    selected = observe_selected_system(target_system)
    observation_count += selected["count"]
    emit_update("PLOTTING_ROUTE", target_system, command="HOLD_SELECT", observation=selected["observation"], reason="SELECTED_SYSTEM_EXACT_NAME_CONFIRMED")
    action.call(id="elite-dangerous/ui-select-hold", inputs={"holdMs": 1000})

    plan = None
    for _ in range(ROUTE_POLL_LIMIT):
        route = route_attempt(target_system, max_jumps)
        observation_count += 1
        emit_update("VERIFYING_ROUTE", target_system, observation=route["plan"] if route["ok"] else None, reason="EXACT_NAV_ROUTE" if route["ok"] else route["reason"])
        if route["ok"]:
            plan = route["plan"]
            break
        task.sleep(milliseconds=OCR_SETTLE_MS)
    if plan == None:
        fail("held PLOT ROUTE was not followed by an exact matching NavRoute")

    emit_update("RESTORING_VIEW", target_system, command="OPEN_GALAXY_MAP", observation=plan, reason="ROUTE_VERIFIED")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "OPEN_GALAXY_MAP"})
    task.sleep(milliseconds=UI_SETTLE_MS)
    closed = observe_map_stable(target_system, False, "RESTORING_VIEW")
    observation_count += closed["count"]
    action.clear_on_failure()
    stream.activity(message="Exact Galaxy Map route plotted: " + target_system, level="info")
    emit_update("COMPLETED", target_system, observation=plan, reason="PLOTTED_EXACT_ROUTE+FORWARD_VIEW_RESTORED")
    return completed(target_system, "PLOTTED", plan, True, True, opened_map, observation_count)
