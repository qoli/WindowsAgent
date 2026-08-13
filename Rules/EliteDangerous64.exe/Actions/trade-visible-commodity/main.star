UI_SETTLE_MS = 750
OCR_SETTLE_MS = 250
CARGO_POLL_MS = 500
CONTROL_SETTLE_MS = 500
QUANTITY_STEP_MS = 60
CARGO_POLL_LIMIT = 20
LIST_SEARCH_BATCH_SIZE = 10
LIST_SEARCH_BATCH_LIMIT = 18
LIST_SEARCH_STEP_MS = 60
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
LIST_MIN_X = 360.0
LIST_MAX_X = 900.0
LIST_MIN_Y = 250.0
LIST_MAX_Y = 900.0

def emit_update(phase, operation, commodity_name, quantity, command=None, observation=None, reason=None):
    stream.emit(
        type="action.trade-visible-commodity.update",
        payload={
            "phase": phase,
            "operation": operation,
            "commodityName": commodity_name,
            "quantity": quantity,
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

def inspect_market(raw, operation, commodity_name, station_name):
    expected_commodity = normalize_text(commodity_name)
    expected_station = normalize_text(station_name)
    expected_mode = "BUYFROMMARKET" if operation == "BUY" else "SELLTOMARKET"
    market_title = False
    station_confirmed = False
    mode_confirmed = False
    exact = []
    trusted_text = []
    for region in raw["regions"]:
        if not trusted(region):
            continue
        normalized = normalize_text(region["text"])
        box = bounds(region)
        trusted_text.append(region["text"])
        if normalized == "COMMODITIESMARKET":
            market_title = True
        if normalized == expected_station:
            station_confirmed = True
        if normalized == expected_mode:
            mode_confirmed = True
        if normalized == expected_commodity and box["centerX"] >= LIST_MIN_X and box["centerX"] <= LIST_MAX_X and box["centerY"] >= LIST_MIN_Y and box["centerY"] <= LIST_MAX_Y:
            exact.append({
                "text": region["text"],
                "bounds": box,
                "detectionConfidence": region["detectionConfidence"],
                "recognitionConfidence": region["recognitionConfidence"],
            })
    return {
        "marketTitle": market_title,
        "stationConfirmed": station_confirmed,
        "modeConfirmed": mode_confirmed,
        "exactCommodityCount": len(exact),
        "commodity": exact[0] if len(exact) == 1 else None,
        "trustedText": trusted_text,
    }

def inspect_dialog(raw, operation, commodity_name):
    expected_commodity = normalize_text(commodity_name)
    expected_title = "BUYCOMMODITY" if operation == "BUY" else "SELLCOMMODITY"
    commodity_confirmed = False
    title_confirmed = False
    for region in raw["regions"]:
        if not trusted(region):
            continue
        normalized = normalize_text(region["text"])
        if normalized == expected_commodity:
            commodity_confirmed = True
        if normalized == expected_title:
            title_confirmed = True
    return {"commodityConfirmed": commodity_confirmed, "titleConfirmed": title_confirmed}

def observe_market_once(operation, commodity_name, station_name):
    header = action.call(id="elite-dangerous/commodity-market-header-text-regions", inputs={})
    upper = action.call(id="elite-dangerous/commodity-market-text-regions", inputs={})
    lower = action.call(id="elite-dangerous/commodity-market-list-text-regions", inputs={})
    return inspect_market({"regions": header["regions"] + upper["regions"] + lower["regions"]}, operation, commodity_name, station_name)

def market_context_confirmed(observation):
    return observation["marketTitle"] and observation["stationConfirmed"] and observation["modeConfirmed"]

def locate_commodity(operation, commodity_name, quantity, station_name):
    observation_count = 0
    previous_context = False
    initial = None
    for _ in range(6):
        current = observe_market_once(operation, commodity_name, station_name)
        observation_count += 1
        if current["exactCommodityCount"] > 1:
            fail("Commodity Market showed ambiguous duplicate exact commodity rows")
        context = market_context_confirmed(current)
        emit_update("CONFIRMING_MARKET", operation, commodity_name, quantity, observation=current, reason="EXACT_MARKET_CONTEXT_BEFORE_BOUNDED_LIST_SEARCH")
        if context and previous_context:
            initial = current
            break
        previous_context = context
        task.sleep(milliseconds=OCR_SETTLE_MS)
    if initial == None:
        fail("Commodity Market did not confirm the exact Station and mode before list search")

    if initial["exactCommodityCount"] == 1:
        return {"observation": initial, "count": observation_count, "navigationSteps": 0}

    mode_y = 252 if operation == "BUY" else 401
    emit_update("FOCUSING_LIST", operation, commodity_name, quantity, command="POINTER_FOCUS_MODE_TILE", observation={"x": 174, "y": mode_y}, reason="RESET_MARKET_FOCUS_BEFORE_BOUNDED_LIST_SEARCH")
    action.call(id="elite-dangerous/pointer-click", inputs={"x": 174, "y": mode_y, "holdMs": 40})
    task.sleep(milliseconds=UI_SETTLE_MS)
    emit_update("FOCUSING_LIST", operation, commodity_name, quantity, command="RIGHT", observation=None, reason="ENTER_FIRST_COMMODITY_ROW_FROM_MODE_TILE")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "RIGHT"})
    task.sleep(milliseconds=CONTROL_SETTLE_MS)

    navigation_steps = 0
    for batch in range(LIST_SEARCH_BATCH_LIMIT + 1):
        current = observe_market_once(operation, commodity_name, station_name)
        observation_count += 1
        if not market_context_confirmed(current):
            fail("Commodity Market context changed during bounded list search")
        if current["exactCommodityCount"] > 1:
            fail("Commodity Market showed ambiguous duplicate exact commodity rows")
        emit_update("SEARCHING_LIST", operation, commodity_name, quantity, observation={"batch": batch, "navigationSteps": navigation_steps, "market": current}, reason="EXACT_COMMODITY_OCR_AFTER_BOUNDED_NAVIGATION")
        if current["exactCommodityCount"] == 1:
            confirmation = observe_market_once(operation, commodity_name, station_name)
            observation_count += 1
            emit_update("SEARCHING_LIST", operation, commodity_name, quantity, observation={"batch": batch, "navigationSteps": navigation_steps, "market": confirmation}, reason="SECOND_EXACT_COMMODITY_OBSERVATION_WITHOUT_INTERVENING_INPUT")
            if market_context_confirmed(confirmation) and confirmation["exactCommodityCount"] == 1:
                return {"observation": confirmation, "count": observation_count, "navigationSteps": navigation_steps}
        if batch == LIST_SEARCH_BATCH_LIMIT:
            break
        for _ in range(LIST_SEARCH_BATCH_SIZE):
            action.call(id="elite-dangerous/ui-control", inputs={"control": "DOWN"})
            navigation_steps += 1
            task.sleep(milliseconds=LIST_SEARCH_STEP_MS)
        emit_update("SCROLLING_LIST", operation, commodity_name, quantity, command="DOWN_X_10", observation={"batch": batch + 1, "navigationSteps": navigation_steps}, reason="BOUNDED_KEYBOARD_LIST_NAVIGATION")
        task.sleep(milliseconds=CONTROL_SETTLE_MS)
    fail("exact commodity was not found within the bounded Commodity Market list search")

def observe_dialog_stable(operation, commodity_name, quantity):
    previous = False
    last = None
    count = 0
    for _ in range(6):
        raw = action.call(id="elite-dangerous/commodity-market-text-regions", inputs={})
        last = inspect_dialog(raw, operation, commodity_name)
        count += 1
        known = last["commodityConfirmed"] and last["titleConfirmed"]
        emit_update("CONFIRMING_DIALOG", operation, commodity_name, quantity, observation=last, reason="EXACT_TRADE_DIALOG_REQUIRED")
        if known and previous:
            return {"observation": last, "count": count}
        previous = known
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("trade dialog did not show the exact commodity and operation")

def commodity_count(cargo, commodity_name):
    if cargo["state"] != "AVAILABLE" or cargo["data"] == None:
        fail("Cargo.json must be AVAILABLE")
    expected = normalize_text(commodity_name)
    count = 0
    matches = 0
    for item in cargo["data"]["Inventory"]:
        localized = item["Name_Localised"] if "Name_Localised" in item else item["Name"]
        if normalize_text(localized) == expected:
            matches += 1
            count += item["Count"]
    if matches > 1:
        fail("Cargo.json contains duplicate exact commodity entries")
    return {"count": count, "timestamp": cargo["data"]["timestamp"], "freshness": cargo["freshness"]}

def observe_market_absent(operation, commodity_name, quantity):
    previous = False
    count = 0
    for _ in range(6):
        raw = action.call(id="elite-dangerous/commodity-market-header-text-regions", inputs={})
        present = False
        for region in raw["regions"]:
            if trusted(region) and normalize_text(region["text"]) == "COMMODITIESMARKET":
                present = True
        count += 1
        emit_update("RESTORING_COCKPIT", operation, commodity_name, quantity, observation={"marketPresent": present}, reason="COMMODITY_MARKET_MUST_BE_ABSENT")
        if not present and previous:
            return count
        previous = not present
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("Commodity Market remained visible after cleanup")

def main(ctx):
    operation = ctx.inputs["operation"]
    commodity_name = ctx.inputs["commodityName"]
    quantity = int(ctx.inputs["quantity"])
    station_name = ctx.inputs["stationName"]

    before_raw = action.call(id="elite-dangerous/filesystem/cargo", inputs={})
    before = commodity_count(before_raw, commodity_name)
    if operation == "SELL" and before["count"] < quantity:
        fail("Cargo.json does not contain enough of the exact commodity to sell")
    emit_update("PREFLIGHT", operation, commodity_name, quantity, observation=before, reason="CURRENT_CARGO_BASELINE")

    visible = locate_commodity(operation, commodity_name, quantity, station_name)
    observation_count = visible["count"] + 1
    commodity = visible["observation"]["commodity"]
    click_x = int(commodity["bounds"]["centerX"])
    click_y = int(commodity["bounds"]["centerY"])

    # The shared cleanup Action spaces its two BACK inputs across the actual UI
    # transitions. It is replaced by explicit normal cleanup below.
    action.on_failure(id="elite-dangerous/exit-commodity-market", inputs={"dialogMayBeOpen": True})

    emit_update("OPENING_DIALOG", operation, commodity_name, quantity, command="POINTER_CLICK", observation={"x": click_x, "y": click_y, "commodity": commodity}, reason="SAME_FRAME_EXACT_OCR_BOX")
    action.call(id="elite-dangerous/pointer-click", inputs={"x": click_x, "y": click_y, "holdMs": 40})
    task.sleep(milliseconds=CONTROL_SETTLE_MS)
    emit_update("OPENING_DIALOG", operation, commodity_name, quantity, command="SELECT", observation={"x": click_x, "y": click_y}, reason="POINTER_ESTABLISHES_FOCUS_AND_BOUND_UI_SELECT_ACTIVATES")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    task.sleep(milliseconds=UI_SETTLE_MS)
    dialog = observe_dialog_stable(operation, commodity_name, quantity)
    observation_count += dialog["count"]

    for index in range(quantity):
        step = index + 1
        if step == 1 or step == quantity or step % 25 == 0:
            emit_update("SETTING_QUANTITY", operation, commodity_name, quantity, command="RIGHT", observation={"step": step, "total": quantity}, reason="EXACT_REQUESTED_QUANTITY")
        action.call(id="elite-dangerous/ui-control", inputs={"control": "RIGHT"})
        task.sleep(milliseconds=QUANTITY_STEP_MS)
    task.sleep(milliseconds=CONTROL_SETTLE_MS)
    emit_update("SUBMITTING", operation, commodity_name, quantity, command="DOWN", observation=dialog["observation"], reason="FOCUS_MATCHING_CONFIRM_BUTTON")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "DOWN"})
    task.sleep(milliseconds=CONTROL_SETTLE_MS)
    emit_update("SUBMITTING", operation, commodity_name, quantity, command="SELECT", observation=None, reason="ONE_CONFIRMED_TRADE_SUBMISSION")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})

    expected_after = before["count"] + quantity if operation == "BUY" else before["count"] - quantity
    after = None
    for _ in range(CARGO_POLL_LIMIT):
        task.sleep(milliseconds=CARGO_POLL_MS)
        attempt = action.call(id="elite-dangerous/filesystem/cargo", inputs={})
        current = commodity_count(attempt, commodity_name)
        observation_count += 1
        emit_update("VERIFYING_CARGO", operation, commodity_name, quantity, observation={"before": before, "current": current, "expectedCount": expected_after}, reason="NEW_CARGO_SNAPSHOT_REQUIRED")
        if current["timestamp"] != before["timestamp"] and current["count"] == expected_after:
            after = current
            break
    if after == None:
        fail("trade submission was not followed by the exact new Cargo count")

    emit_update("RESTORING_COCKPIT", operation, commodity_name, quantity, command="EXIT_COMMODITY_MARKET", observation=after, reason="LEAVE_MARKET_AND_STARPORT_SERVICES")
    action.call(id="elite-dangerous/exit-commodity-market", inputs={"dialogMayBeOpen": False})
    observation_count += observe_market_absent(operation, commodity_name, quantity)
    action.clear_on_failure()

    emit_update("COMPLETED", operation, commodity_name, quantity, observation={"before": before, "after": after}, reason="EXACT_CARGO_DELTA_AND_COMMODITY_MARKET_ABSENT")
    stream.activity(message=operation + " confirmed: " + str(quantity) + "t " + commodity_name, level="info")
    return {
        "schemaVersion": 1,
        "task": "TRADE_VISIBLE_COMMODITY",
        "completed": True,
        "operation": operation,
        "commodityName": commodity_name,
        "quantity": quantity,
        "stationName": station_name,
        "beforeCount": before["count"],
        "afterCount": after["count"],
        "beforeTimestamp": before["timestamp"],
        "afterTimestamp": after["timestamp"],
        "marketModeConfirmed": True,
        "dialogConfirmed": True,
        "commodityMarketAbsent": True,
        "observationCount": observation_count,
    }
